// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package catalog

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"time"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"

	pkgcatalog "github.com/matrixorigin/matrixone/pkg/catalog"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/containers"
)

type IndexT uint16

const (
	ZoneMap IndexT = iota
)

func i82bool(v int8) bool {
	return v == 1
}

type IndexInfo struct {
	Id      uint64
	Name    string
	Type    IndexT
	Columns []uint16
}

type Default struct {
	NullAbility  bool
	Expr         []byte
	OriginString string
}

func (d *Default) Marshal() ([]byte, error) {
	expr := &plan.Expr{}
	if d.Expr != nil {
		if err := expr.Unmarshal(d.Expr); err != nil {
			logutil.Warnf("deserialze default expr err: %v", err)
			expr = nil
		}
	} else {
		expr = nil
	}
	pDefault := &plan.Default{
		NullAbility:  d.NullAbility,
		OriginString: d.OriginString,
		Expr:         expr,
	}
	return types.Encode(pDefault)
}

func (d *Default) Unmarshal(data []byte) (err error) {
	if len(data) != len([]byte("")) {
		pDefault := new(plan.Default)
		if err = types.Decode(data, pDefault); err != nil {
			return
		}
		d.NullAbility = pDefault.NullAbility
		d.OriginString = pDefault.OriginString
		d.Expr = nil
		if pDefault.Expr != nil {
			if d.Expr, err = pDefault.Expr.Marshal(); err != nil {
				return
			}
		}
	}
	return nil
}

type OnUpdate struct {
	Expr         []byte
	OriginString string
}

func (u *OnUpdate) Marshal() ([]byte, error) {
	expr := &plan.Expr{}
	if u.Expr != nil {
		if err := expr.Unmarshal(u.Expr); err != nil {
			logutil.Warnf("deserialze onUpdate expr err: %v", err)
			expr = nil
		}
	} else {
		expr = nil
	}
	pUpdate := &plan.OnUpdate{
		OriginString: u.OriginString,
		Expr:         expr,
	}
	return types.Encode(pUpdate)
}
func (u *OnUpdate) Unmarshal(data []byte) (err error) {
	if len(data) != len([]byte("")) {
		pUpdate := &plan.OnUpdate{}
		if err = types.Decode(data, pUpdate); err != nil {
			return
		}
		u.OriginString = pUpdate.OriginString
		u.Expr = nil
		if pUpdate.Expr != nil {
			if u.Expr, err = pUpdate.Expr.Marshal(); err != nil {
				return
			}
		}
	}
	return
}
func NewIndexInfo(name string, typ IndexT, colIdx ...int) *IndexInfo {
	index := &IndexInfo{
		Name:    name,
		Type:    typ,
		Columns: make([]uint16, 0),
	}
	for _, col := range colIdx {
		index.Columns = append(index.Columns, uint16(col))
	}
	return index
}

type ColDef struct {
	Name          string
	Idx           int // indicates its position in all coldefs
	Type          types.Type
	Hidden        bool // Hidden Column is generated by compute layer, keep hidden from user
	PhyAddr       bool // PhyAddr Column is generated by tae as rowid
	NullAbility   bool
	AutoIncrement bool
	Primary       bool
	SortIdx       int8 // indicates its position in all sort keys
	SortKey       bool
	Comment       string
	Default       Default
	OnUpdate      OnUpdate
}

func (def *ColDef) GetName() string     { return def.Name }
func (def *ColDef) GetType() types.Type { return def.Type }

func (def *ColDef) Nullable() bool        { return def.NullAbility }
func (def *ColDef) IsHidden() bool        { return def.Hidden }
func (def *ColDef) IsPhyAddr() bool       { return def.PhyAddr }
func (def *ColDef) IsPrimary() bool       { return def.Primary }
func (def *ColDef) IsAutoIncrement() bool { return def.AutoIncrement }
func (def *ColDef) IsSortKey() bool       { return def.SortKey }

type SortKey struct {
	Defs      []*ColDef
	search    map[int]int
	isPrimary bool
}

func NewSortKey() *SortKey {
	return &SortKey{
		Defs:   make([]*ColDef, 0),
		search: make(map[int]int),
	}
}

func (cpk *SortKey) AddDef(def *ColDef) (ok bool) {
	_, found := cpk.search[def.Idx]
	if found {
		return false
	}
	if def.IsPrimary() {
		cpk.isPrimary = true
	}
	cpk.Defs = append(cpk.Defs, def)
	sort.Slice(cpk.Defs, func(i, j int) bool { return cpk.Defs[i].SortIdx < cpk.Defs[j].SortIdx })
	cpk.search[def.Idx] = int(def.SortIdx)
	return true
}

func (cpk *SortKey) IsPrimary() bool                { return cpk.isPrimary }
func (cpk *SortKey) Size() int                      { return len(cpk.Defs) }
func (cpk *SortKey) GetDef(pos int) *ColDef         { return cpk.Defs[pos] }
func (cpk *SortKey) HasColumn(idx int) (found bool) { _, found = cpk.search[idx]; return }
func (cpk *SortKey) GetSingleIdx() int              { return cpk.Defs[0].Idx }

type Schema struct {
	AcInfo           accessInfo
	Name             string
	ColDefs          []*ColDef
	NameIndex        map[string]int
	BlockMaxRows     uint32
	SegmentMaxBlocks uint16
	Comment          string
	Partition        string
	Relkind          string
	Createsql        string
	View             string
	IndexInfos       []*ComputeIndexInfo

	SortKey    *SortKey
	PhyAddrKey *ColDef
}

func NewEmptySchema(name string) *Schema {
	return &Schema{
		Name:       name,
		ColDefs:    make([]*ColDef, 0),
		NameIndex:  make(map[string]int),
		IndexInfos: make([]*ComputeIndexInfo, 0),
	}
}

func (s *Schema) Clone() *Schema {
	buf, err := s.Marshal()
	if err != nil {
		panic(err)
	}
	ns := NewEmptySchema(s.Name)
	r := bytes.NewBuffer(buf)
	if _, err = ns.ReadFrom(r); err != nil {
		panic(err)
	}
	return ns
}
func (s *Schema) GetSortKeyType() types.Type {
	return s.GetSingleSortKey().Type
}
func (s *Schema) HasPK() bool      { return s.SortKey != nil && s.SortKey.IsPrimary() }
func (s *Schema) HasSortKey() bool { return s.SortKey != nil }

// GetSingleSortKey should be call only if IsSinglePK is checked
func (s *Schema) GetSingleSortKey() *ColDef { return s.SortKey.Defs[0] }
func (s *Schema) GetSingleSortKeyIdx() int  { return s.SortKey.Defs[0].Idx }

func MarshalOnUpdate(w *bytes.Buffer, data OnUpdate) (err error) {
	if err = binary.Write(w, binary.BigEndian, uint16(len([]byte(data.OriginString)))); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, []byte(data.OriginString)); err != nil {
		return
	}
	if data.Expr == nil {
		err = binary.Write(w, binary.BigEndian, uint16(0))
		return
	} else {
		if err = binary.Write(w, binary.BigEndian, uint16(len(data.Expr))); err != nil {
			return
		}
	}
	if err = binary.Write(w, binary.BigEndian, data.Expr); err != nil {
		return
	}
	return nil
}

func UnMarshalOnUpdate(r io.Reader, data *OnUpdate) (n int64, err error) {

	var valueLen uint16 = 0
	if err = binary.Read(r, binary.BigEndian, &valueLen); err != nil {
		return
	}
	n = 2

	buf := make([]byte, valueLen)
	if _, err = r.Read(buf); err != nil {
		return
	}
	data.OriginString = string(buf)
	n += int64(valueLen)

	valueLen = 0
	if err = binary.Read(r, binary.BigEndian, &valueLen); err != nil {
		return
	}
	n += 2

	if valueLen == 0 {
		data.Expr = nil
		return
	}

	buf = make([]byte, valueLen)
	if _, err = r.Read(buf); err != nil {
		return
	}
	data.Expr = buf
	n += int64(valueLen)
	return n, nil
}

func MarshalDefault(w *bytes.Buffer, data Default) (err error) {
	if err = binary.Write(w, binary.BigEndian, data.NullAbility); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, uint16(len([]byte(data.OriginString)))); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, []byte(data.OriginString)); err != nil {
		return
	}
	if data.Expr == nil {
		err = binary.Write(w, binary.BigEndian, uint16(0))
		return
	} else {
		if err = binary.Write(w, binary.BigEndian, uint16(len(data.Expr))); err != nil {
			return
		}
	}

	if err = binary.Write(w, binary.BigEndian, data.Expr); err != nil {
		return
	}
	return nil
}

func UnMarshalDefault(r io.Reader, data *Default) (n int64, err error) {
	if err = binary.Read(r, binary.BigEndian, &data.NullAbility); err != nil {
		return
	}
	n = 1

	var valueLen uint16 = 0
	if err = binary.Read(r, binary.BigEndian, &valueLen); err != nil {
		return
	}
	n += 2

	buf := make([]byte, valueLen)
	if _, err = r.Read(buf); err != nil {
		return
	}
	data.OriginString = string(buf)
	n += int64(valueLen)

	valueLen = 0
	if err = binary.Read(r, binary.BigEndian, &valueLen); err != nil {
		return
	}
	n += 2

	if valueLen == 0 {
		data.Expr = nil
		return
	}

	buf = make([]byte, valueLen)
	if _, err = r.Read(buf); err != nil {
		return
	}
	data.Expr = buf
	n += int64(valueLen)
	return n, nil
}

func (s *Schema) ReadFrom(r io.Reader) (n int64, err error) {
	if err = binary.Read(r, binary.BigEndian, &s.BlockMaxRows); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &s.SegmentMaxBlocks); err != nil {
		return
	}
	n = 4 + 4
	var sn int64
	if sn, err = s.AcInfo.ReadFrom(r); err != nil {
		return
	}
	n += sn
	if s.Name, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn
	if s.Comment, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn
	if s.Partition, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn
	if s.Relkind, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn
	if s.Createsql, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn

	if s.View, sn, err = common.ReadString(r); err != nil {
		return
	}
	n += sn
	colCnt := uint16(0)
	if err = binary.Read(r, binary.BigEndian, &colCnt); err != nil {
		return
	}
	n += 2
	colBuf := make([]byte, types.TSize)
	for i := uint16(0); i < colCnt; i++ {
		if _, err = r.Read(colBuf); err != nil {
			return
		}
		n += int64(types.TSize)
		def := new(ColDef)
		def.Type = types.DecodeType(colBuf)
		if def.Name, sn, err = common.ReadString(r); err != nil {
			return
		}
		n += sn
		if def.Comment, sn, err = common.ReadString(r); err != nil {
			return
		}
		n += sn
		if err = binary.Read(r, binary.BigEndian, &def.NullAbility); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.Hidden); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.PhyAddr); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.AutoIncrement); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.SortIdx); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.Primary); err != nil {
			return
		}
		n += 1
		if err = binary.Read(r, binary.BigEndian, &def.SortKey); err != nil {
			return
		}
		n += 1
		def.Default = Default{}
		length := uint64(0)
		if err = binary.Read(r, binary.BigEndian, &length); err != nil {
			return
		}
		n += 8
		buf := make([]byte, length)
		var sn2 int
		if sn2, err = r.Read(buf); err != nil {
			return
		}
		n += int64(sn2)
		if err = def.Default.Unmarshal(buf); err != nil {
			return
		}
		def.OnUpdate = OnUpdate{}
		length = uint64(0)
		if err = binary.Read(r, binary.BigEndian, &length); err != nil {
			return
		}
		n += 8
		buf = make([]byte, length)
		if sn2, err = r.Read(buf); err != nil {
			return
		}
		n += int64(sn2)
		if err = def.OnUpdate.Unmarshal(buf); err != nil {
			return
		}
		if err = s.AppendColDef(def); err != nil {
			return
		}
	}
	err = s.Finalize(true)
	return
}

func (s *Schema) Marshal() (buf []byte, err error) {
	var w bytes.Buffer
	if err = binary.Write(&w, binary.BigEndian, s.BlockMaxRows); err != nil {
		return
	}
	if err = binary.Write(&w, binary.BigEndian, s.SegmentMaxBlocks); err != nil {
		return
	}
	if _, err = s.AcInfo.WriteTo(&w); err != nil {
		return
	}
	if _, err = common.WriteString(s.Name, &w); err != nil {
		return
	}
	if _, err = common.WriteString(s.Comment, &w); err != nil {
		return
	}
	if _, err = common.WriteString(s.Partition, &w); err != nil {
		return
	}
	if _, err = common.WriteString(s.Relkind, &w); err != nil {
		return
	}
	if _, err = common.WriteString(s.Createsql, &w); err != nil {
		return
	}
	if _, err = common.WriteString(s.View, &w); err != nil {
		return
	}
	if err = binary.Write(&w, binary.BigEndian, uint16(len(s.ColDefs))); err != nil {
		return
	}
	for _, def := range s.ColDefs {
		if _, err = w.Write(types.EncodeType(&def.Type)); err != nil {
			return
		}
		if _, err = common.WriteString(def.Name, &w); err != nil {
			return
		}
		if _, err = common.WriteString(def.Comment, &w); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.NullAbility); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.Hidden); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.PhyAddr); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.AutoIncrement); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.SortIdx); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.Primary); err != nil {
			return
		}
		if err = binary.Write(&w, binary.BigEndian, def.SortKey); err != nil {
			return
		}
		var data []byte
		data, err = def.Default.Marshal()
		if err != nil {
			data = []byte("")
		}
		length := uint64(len(data))
		if err = binary.Write(&w, binary.BigEndian, length); err != nil {
			return
		}
		if _, err = w.Write(data); err != nil {
			return
		}
		data, err = def.OnUpdate.Marshal()
		if err != nil {
			data = []byte("")
		}
		length = uint64(len(data))
		if err = binary.Write(&w, binary.BigEndian, length); err != nil {
			return
		}
		if _, err = w.Write(data); err != nil {
			return
		}
	}
	buf = w.Bytes()
	return
}

func (s *Schema) ReadFromBatch(bat *containers.Batch, offset int) (next int) {
	nameVec := bat.GetVectorByName(pkgcatalog.SystemColAttr_RelName)
	tidVec := bat.GetVectorByName(pkgcatalog.SystemColAttr_RelID)
	tid := tidVec.Get(offset).(uint64)
	for {
		if offset >= nameVec.Length() {
			break
		}
		name := string(nameVec.Get(offset).([]byte))
		id := tidVec.Get(offset).(uint64)
		if name != s.Name || id != tid {
			break
		}
		def := new(ColDef)
		def.Name = string(bat.GetVectorByName((pkgcatalog.SystemColAttr_Name)).Get(offset).([]byte))
		data := bat.GetVectorByName((pkgcatalog.SystemColAttr_Type)).Get(offset).([]byte)
		types.Decode(data, &def.Type)
		data = bat.GetVectorByName((pkgcatalog.SystemColAttr_DefaultExpr)).Get(offset).([]byte)
		err := def.Default.Unmarshal(data)
		if err != nil {
			panic(err)
		}
		nullable := bat.GetVectorByName((pkgcatalog.SystemColAttr_NullAbility)).Get(offset).(int8)
		def.NullAbility = i82bool(nullable)
		isHidden := bat.GetVectorByName((pkgcatalog.SystemColAttr_IsHidden)).Get(offset).(int8)
		def.Hidden = i82bool(isHidden)
		isAutoIncrement := bat.GetVectorByName((pkgcatalog.SystemColAttr_IsAutoIncrement)).Get(offset).(int8)
		def.AutoIncrement = i82bool(isAutoIncrement)
		def.Comment = string(bat.GetVectorByName((pkgcatalog.SystemColAttr_Comment)).Get(offset).([]byte))
		data = bat.GetVectorByName((pkgcatalog.SystemColAttr_Update)).Get(offset).([]byte)
		if err = def.OnUpdate.Unmarshal(data); err != nil {
			panic(err)
		}
		idx := bat.GetVectorByName((pkgcatalog.SystemColAttr_Num)).Get(offset).(int32)
		s.NameIndex[def.Name] = int(idx - 1)
		def.Idx = int(idx - 1)
		s.ColDefs = append(s.ColDefs, def)
		if def.Name == PhyAddrColumnName {
			def.PhyAddr = true
			s.PhyAddrKey = def
		}
		constraint := string(bat.GetVectorByName(pkgcatalog.SystemColAttr_ConstraintType).Get(offset).([]byte))
		if constraint == "p" {
			def.SortKey = true
			def.Primary = true
			if s.SortKey == nil {
				s.SortKey = NewSortKey()
			}
			s.SortKey.AddDef(def)
		}
		offset++
	}
	return offset
}

func (s *Schema) AppendColDef(def *ColDef) (err error) {
	def.Idx = len(s.ColDefs)
	s.ColDefs = append(s.ColDefs, def)
	_, existed := s.NameIndex[def.Name]
	if existed {
		err = moerr.NewConstraintViolation("duplicate column \"%s\"", def.Name)
		return
	}
	s.NameIndex[def.Name] = def.Idx
	return
}

func (s *Schema) AppendSortKey(name string, typ types.Type, idx int, isPrimary bool) error {
	def := &ColDef{
		Name:    name,
		Type:    typ,
		SortIdx: int8(idx),
		SortKey: true,
	}
	def.Primary = isPrimary
	return s.AppendColDef(def)
}

func (s *Schema) AppendPKCol(name string, typ types.Type, idx int) error {
	def := &ColDef{
		Name:        name,
		Type:        typ,
		SortIdx:     int8(idx),
		SortKey:     true,
		Primary:     true,
		NullAbility: false,
	}
	return s.AppendColDef(def)
}

func (s *Schema) AppendPKColWithAttribute(attr engine.Attribute, idx int) error {
	var bs []byte = nil
	var err error
	if attr.Default.Expr != nil {
		bs, err = attr.Default.Expr.Marshal()
		if err != nil {
			return err
		}
	}
	attrDefault := Default{
		NullAbility:  attr.Default.NullAbility,
		Expr:         bs,
		OriginString: attr.Default.OriginString,
	}
	var attrOnUpdate OnUpdate
	if attr.OnUpdate != nil && attr.OnUpdate.Expr != nil {
		ps, err := attr.OnUpdate.Expr.Marshal()
		if err != nil {
			return err
		}
		attrOnUpdate = OnUpdate{
			Expr:         ps,
			OriginString: attr.OnUpdate.OriginString,
		}
	}
	def := &ColDef{
		Name:          attr.Name,
		Type:          attr.Type,
		SortIdx:       int8(idx),
		Hidden:        attr.IsHidden,
		SortKey:       true,
		Primary:       true,
		Comment:       attr.Comment,
		NullAbility:   attrDefault.NullAbility,
		Default:       attrDefault,
		AutoIncrement: attr.AutoIncrement,
		OnUpdate:      attrOnUpdate,
	}
	return s.AppendColDef(def)
}

func (s *Schema) AppendCol(name string, typ types.Type) error {
	def := &ColDef{
		Name:        name,
		Type:        typ,
		SortIdx:     -1,
		NullAbility: true,
	}
	return s.AppendColDef(def)
}

func (s *Schema) AppendColWithDefault(name string, typ types.Type, val Default) error {
	def := &ColDef{
		Name:        name,
		Type:        typ,
		SortIdx:     -1,
		Default:     val,
		NullAbility: val.NullAbility,
	}
	return s.AppendColDef(def)
}

func (s *Schema) AppendColWithAttribute(attr engine.Attribute) error {
	var bs []byte = nil
	var err error
	if attr.Default.Expr != nil {
		bs, err = attr.Default.Expr.Marshal()
		if err != nil {
			return err
		}
	}
	attrDefault := Default{
		NullAbility:  attr.Default.NullAbility,
		Expr:         bs,
		OriginString: attr.Default.OriginString,
	}
	var attrOnUpdate OnUpdate
	if attr.OnUpdate != nil && attr.OnUpdate.Expr != nil {
		attrOnUpdate.Expr, err = attr.OnUpdate.Expr.Marshal()
		if err != nil {
			return err
		}
		attrOnUpdate.OriginString = attr.OnUpdate.OriginString
	}
	def := &ColDef{
		Name:          attr.Name,
		Type:          attr.Type,
		Hidden:        attr.IsHidden,
		SortIdx:       -1,
		Comment:       attr.Comment,
		Default:       attrDefault,
		NullAbility:   attrDefault.NullAbility,
		AutoIncrement: attr.AutoIncrement,
		OnUpdate:      attrOnUpdate,
	}
	return s.AppendColDef(def)
}

func (s *Schema) String() string {
	buf, _ := json.Marshal(s)
	return string(buf)
}

func (s *Schema) IsPartOfPK(idx int) bool {
	return s.ColDefs[idx].IsPrimary()
}

func (s *Schema) Attrs() []string {
	if len(s.ColDefs) == 0 {
		return make([]string, 0)
	}
	attrs := make([]string, 0, len(s.ColDefs)-1)
	for _, def := range s.ColDefs {
		if def.IsPhyAddr() {
			continue
		}
		attrs = append(attrs, def.Name)
	}
	return attrs
}

func (s *Schema) Types() []types.Type {
	if len(s.ColDefs) == 0 {
		return make([]types.Type, 0)
	}
	ts := make([]types.Type, 0, len(s.ColDefs)-1)
	for _, def := range s.ColDefs {
		if def.IsPhyAddr() {
			continue
		}
		ts = append(ts, def.Type)
	}
	return ts
}

func (s *Schema) Nullables() []bool {
	if len(s.ColDefs) == 0 {
		return make([]bool, 0)
	}
	nulls := make([]bool, 0, len(s.ColDefs)-1)
	for _, def := range s.ColDefs {
		if def.IsPhyAddr() {
			continue
		}
		nulls = append(nulls, def.Nullable())
	}
	return nulls
}

func (s *Schema) AllNullables() []bool {
	if len(s.ColDefs) == 0 {
		return make([]bool, 0)
	}
	nulls := make([]bool, 0, len(s.ColDefs))
	for _, def := range s.ColDefs {
		nulls = append(nulls, def.Nullable())
	}
	return nulls
}

func (s *Schema) AllTypes() []types.Type {
	if len(s.ColDefs) == 0 {
		return make([]types.Type, 0)
	}
	ts := make([]types.Type, 0, len(s.ColDefs))
	for _, def := range s.ColDefs {
		ts = append(ts, def.Type)
	}
	return ts
}

func (s *Schema) AllNames() []string {
	if len(s.ColDefs) == 0 {
		return make([]string, 0)
	}
	names := make([]string, 0, len(s.ColDefs))
	for _, def := range s.ColDefs {
		names = append(names, def.Name)
	}
	return names
}

// Finalize runs various checks and create shortcuts to phyaddr and sortkey
func (s *Schema) Finalize(rebuild bool) (err error) {
	if s == nil {
		err = moerr.NewConstraintViolation("no schema")
		return
	}
	if !rebuild {
		phyAddrDef := &ColDef{
			Name:        PhyAddrColumnName,
			Comment:     PhyAddrColumnComment,
			Type:        PhyAddrColumnType,
			Hidden:      true,
			NullAbility: false,
			PhyAddr:     true,
		}
		if err = s.AppendColDef(phyAddrDef); err != nil {
			return
		}
	}
	if len(s.ColDefs) == 0 {
		err = moerr.NewConstraintViolation("no schema")
		return
	}

	// sortIdx is sort key index list. as of now, sort key is pk
	sortIdx := make([]int, 0)
	names := make(map[string]bool)
	for idx, def := range s.ColDefs {
		// Check column idx validility
		if idx != def.Idx {
			return moerr.NewInvalidInput(fmt.Sprintf("schema: wrong column index %d specified for \"%s\"", def.Idx, def.Name))
		}
		// Check unique name
		if _, ok := names[def.Name]; ok {
			return moerr.NewInvalidInput("schema: duplicate column \"%s\"", def.Name)
		}
		names[def.Name] = true
		if def.IsSortKey() {
			sortIdx = append(sortIdx, idx)
		}
		if def.IsPhyAddr() {
			if s.PhyAddrKey != nil {
				return moerr.NewInvalidInput("schema: duplicated physical address column \"%s\"", def.Name)
			}
			s.PhyAddrKey = def
		}
	}

	// TODO: If computation layer gives more than one sortkey(pk), maybe we can assume that
	// there is one and only one whose Hidden is true, and put that one into SortKey.
	// For other codes in tae, like append、mergeblocks, IsSinglePK and IsSingleSortKey is still true.
	// Nothing needs to be changed to support the composite key

	if len(sortIdx) == 1 {
		def := s.ColDefs[sortIdx[0]]
		if def.SortIdx != 0 {
			err = moerr.NewConstraintViolation("bad sort idx %d, should be 0", def.SortIdx)
			return
		}
		s.SortKey = NewSortKey()
		s.SortKey.AddDef(def)
	} else if len(sortIdx) > 1 {
		s.SortKey = NewSortKey()
		for _, idx := range sortIdx {
			def := s.ColDefs[idx]
			if ok := s.SortKey.AddDef(def); !ok { // Fixme: I guess it is impossible to be duplicated here because no duplicated idx?
				return moerr.NewInvalidInput("schema: duplicated sort idx specified")
			}
		}
		isPrimary := s.SortKey.Defs[0].IsPrimary()
		for i, def := range s.SortKey.Defs {
			if int(def.SortIdx) != i {
				err = moerr.NewConstraintViolation("duplicated sort idx specified")
				return
			}
			if def.IsPrimary() != isPrimary {
				err = moerr.NewConstraintViolation("duplicated sort idx specified")
				return
			}
		}
	}
	return
}

// GetColIdx returns column index for the given column name
// if found, otherwise returns -1.
func (s *Schema) GetColIdx(attr string) int {
	idx, ok := s.NameIndex[attr]
	if !ok {
		return -1
	}
	return idx
}

func MockSchema(colCnt int, pkIdx int) *Schema {
	rand.Seed(time.Now().UnixNano())
	schema := NewEmptySchema(time.Now().String())
	prefix := "mock_"
	for i := 0; i < colCnt; i++ {
		if pkIdx == i {
			_ = schema.AppendPKCol(fmt.Sprintf("%s%d", prefix, i), types.Type{Oid: types.T_int32, Size: 4, Width: 4}, 0)
		} else {
			_ = schema.AppendCol(fmt.Sprintf("%s%d", prefix, i), types.Type{Oid: types.T_int32, Size: 4, Width: 4})
		}
	}
	_ = schema.Finalize(false)
	return schema
}

// MockSchemaAll if char/varchar is needed, colCnt = 14, otherwise colCnt = 12
// pkIdx == -1 means no pk defined
func MockSchemaAll(colCnt int, pkIdx int, from ...int) *Schema {
	schema := NewEmptySchema(time.Now().String())
	prefix := "mock_"
	start := 0
	if len(from) > 0 {
		start = from[0]
	}
	for i := 0; i < colCnt; i++ {
		if i < start {
			continue
		}
		name := fmt.Sprintf("%s%d", prefix, i)
		var typ types.Type
		switch i % 18 {
		case 0:
			typ = types.T_int8.ToType()
			typ.Width = 8
		case 1:
			typ = types.T_int16.ToType()
			typ.Width = 16
		case 2:
			typ = types.T_int32.ToType()
			typ.Width = 32
		case 3:
			typ = types.T_int64.ToType()
			typ.Width = 64
		case 4:
			typ = types.T_uint8.ToType()
			typ.Width = 8
		case 5:
			typ = types.T_uint16.ToType()
			typ.Width = 16
		case 6:
			typ = types.T_uint32.ToType()
			typ.Width = 32
		case 7:
			typ = types.T_uint64.ToType()
			typ.Width = 64
		case 8:
			typ = types.T_float32.ToType()
			typ.Width = 32
		case 9:
			typ = types.T_float64.ToType()
			typ.Width = 64
		case 10:
			typ = types.T_date.ToType()
			typ.Width = 32
		case 11:
			typ = types.T_datetime.ToType()
			typ.Width = 64
		case 12:
			typ = types.T_varchar.ToType()
			typ.Width = 100
		case 13:
			typ = types.T_char.ToType()
			typ.Width = 100
		case 14:
			typ = types.T_timestamp.ToType()
			typ.Width = 64
		case 15:
			typ = types.T_decimal64.ToType()
			typ.Width = 64
		case 16:
			typ = types.T_decimal128.ToType()
			typ.Width = 128
		case 17:
			typ = types.T_bool.ToType()
			typ.Width = 8
		}

		if pkIdx == i {
			_ = schema.AppendPKCol(name, typ, 0)
		} else {
			_ = schema.AppendCol(name, typ)
			schema.ColDefs[len(schema.ColDefs)-1].NullAbility = true
		}
	}
	schema.BlockMaxRows = 1000
	schema.SegmentMaxBlocks = 10
	_ = schema.Finalize(false)
	return schema
}

func GetAttrIdx(attrs []string, name string) int {
	for i, attr := range attrs {
		if attr == name {
			return i
		}
	}
	panic("logic error")
}

type ComputeIndexInfo struct {
	Name      string
	TableName string
	Unique    bool
	Field     []string
}
