// Copyright 2022 Matrix Origin
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

package bytejson

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/pingcap/errors"
)

type subPathType byte
type pathFlag byte

type ByteJson struct {
	Data []byte
	Type TpCode
}

type subPathIndices struct {
	tp  byte
	num int
}
type subPathRangeExpr struct {
	start *subPathIndices
	end   *subPathIndices
}

type subPath struct {
	key    string
	idx    *subPathIndices
	iRange *subPathRangeExpr
	tp     subPathType
}
type Path struct {
	paths []subPath
	flag  pathFlag
}
type pathGenerator struct {
	pathStr string
	pos     int
}

type UnnestResult map[string][]byte

const (
	numberIndices byte = iota + 1
	lastIndices
	lastKey    = "last"
	lastKeyLen = 4
	toKey      = "to"
	toKeyLen   = 2
)

const (
	subPathIdxALL = -1
	subPathIdxErr = -2
)

const (
	subPathDoubleStar subPathType = iota + 1
	subPathIdx
	subPathKey
	subPathRange
)
const (
	pathFlagSingleStar pathFlag = iota + 1
	pathFlagDoubleStar
)

type TpCode = byte

const (
	TpCodeObject     TpCode = 0x01
	TpCodeArray      TpCode = 0x03
	TpCodeLiteral    TpCode = 0x04
	TpCodeInt64      TpCode = 0x09
	TpCodeUint64     TpCode = 0x0a
	TpCodeFloat64    TpCode = 0x0b
	TpCodeString     TpCode = 0x0c
	TpCodeOpaque     TpCode = 0x0d
	TypeCodeDate     TpCode = 0x0e
	TypeCodeDateTime TpCode = 0x0f
	TpCodeTimeStmap  TpCode = 0x10
	TpCodeDuring     TpCode = 0x11
)

// var jsonSafeSet = [utf8.RuneSelf]bool{
// 	' ':      true,
// 	'!':      true,
// 	'"':      false,
// 	'#':      true,
// 	'$':      true,
// 	'%':      true,
// 	'&':      true,
// 	'\'':     true,
// 	'(':      true,
// 	')':      true,
// 	'*':      true,
// 	'+':      true,
// 	',':      true,
// 	'-':      true,
// 	'.':      true,
// 	'/':      true,
// 	'0':      true,
// 	'1':      true,
// 	'2':      true,
// 	'3':      true,
// 	'4':      true,
// 	'5':      true,
// 	'6':      true,
// 	'7':      true,
// 	'8':      true,
// 	'9':      true,
// 	':':      true,
// 	';':      true,
// 	'<':      true,
// 	'=':      true,
// 	'>':      true,
// 	'?':      true,
// 	'@':      true,
// 	'A':      true,
// 	'B':      true,
// 	'C':      true,
// 	'D':      true,
// 	'E':      true,
// 	'F':      true,
// 	'G':      true,
// 	'H':      true,
// 	'I':      true,
// 	'J':      true,
// 	'K':      true,
// 	'L':      true,
// 	'M':      true,
// 	'N':      true,
// 	'O':      true,
// 	'P':      true,
// 	'Q':      true,
// 	'R':      true,
// 	'S':      true,
// 	'T':      true,
// 	'U':      true,
// 	'V':      true,
// 	'W':      true,
// 	'X':      true,
// 	'Y':      true,
// 	'Z':      true,
// 	'[':      true,
// 	'\\':     false,
// 	']':      true,
// 	'^':      true,
// 	'_':      true,
// 	'`':      true,
// 	'a':      true,
// 	'b':      true,
// 	'c':      true,
// 	'd':      true,
// 	'e':      true,
// 	'f':      true,
// 	'g':      true,
// 	'h':      true,
// 	'i':      true,
// 	'j':      true,
// 	'k':      true,
// 	'l':      true,
// 	'm':      true,
// 	'n':      true,
// 	'o':      true,
// 	'p':      true,
// 	'q':      true,
// 	'r':      true,
// 	's':      true,
// 	't':      true,
// 	'u':      true,
// 	'v':      true,
// 	'w':      true,
// 	'x':      true,
// 	'y':      true,
// 	'z':      true,
// 	'{':      true,
// 	'|':      true,
// 	'}':      true,
// 	'~':      true,
// 	'\u007f': true,
// }

const (
	headerSize   = 8 // element size + data size.
	dataSizeOff  = 4 //
	keyEntrySize = 6 // keyOff +  keyLen
	keyLenOff    = 4 // offset -> uint32
	valTypeSize  = 1 // TpCode -> byte
	valEntrySize = 5 // TpCode + offset-or-inline-value
	numberSize   = 8 // float64|int64|uint64
)

const (
	LiteralNull  byte = 0x00
	LiteralTrue  byte = 0x01
	LiteralFalse byte = 0x02
)

var (
	//hexChars = "0123456789abcdef"
	jsonEndian = binary.LittleEndian
)

var (
	Null = ByteJson{Type: TpCodeLiteral, Data: []byte{LiteralNull}}
)

var (
	escapedChars = map[byte]byte{
		'"': '"',
		'b': '\b',
		'f': '\f',
		'n': '\n',
		'r': '\r',
		't': '\t',
	}
)

var jsonZero = CreateByteJSON(uint64(0))

func CreateByteJSON(in any) ByteJson {
	bj, err := CreateByteJsonWithCheck(in)
	if err != nil {
		panic(err)
	}
	return bj
}

func CreateByteJsonWithCheck(in any) (ByteJson, error) {
	typeCode, buf, err := appendByteJSON(nil, in)
	if err != nil {
		return ByteJson{}, err
	}
	bj := ByteJson{TypeCode: typeCode, Value: buf}
	// GetElemDepth always returns +1.
	if bj.GetElemDepth()-1 > maxJSONDepth {
		return ByteJson{}, ErrJSONDocumentTooDeep
	}
	return bj, nil
}

func appendByteJSON(buf []byte, in any) (TpCode, []byte, error) {
	var typeCode byte
	var err error
	switch x := in.(type) {
	case nil:
		typeCode = TpCodeLiteral
		buf = append(buf, LiteralNull)
	case bool:
		typeCode = TpCodeLiteral
		if x {
			buf = append(buf, LiteralTrue)
		} else {
			buf = append(buf, LiteralFalse)
		}
	case int64:
		typeCode = TpCodeInt64
		buf = appendBinaryUint64(buf, uint64(x))
	case uint64:
		typeCode = TpCodeUint64
		buf = appendBinaryUint64(buf, x)
	case float64:
		typeCode = TpCodeFloat64
		buf = appendBinaryFloat64(buf, x)
	case json.Number:
		typeCode, buf, err = appendBinaryNumber(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case string:
		typeCode = TpCodeString
		buf = appendBinaryString(buf, x)
	case ByteJson:
		typeCode = x.Type
		buf = append(buf, x.Data...)
	case []any:
		typeCode = TpCodeArray
		buf, err = appendBinaryArray(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case map[string]any:
		typeCode = TpCodeObject
		buf, err = appendBinaryObject(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case Opaque:
		typeCode = JSONTypeCodeOpaque
		buf = appendBinaryOpaque(buf, x)
	case Time:
		typeCode = JSONTypeCodeDate
		if x.Type() == mysql.TypeDatetime {
			typeCode = JSONTypeCodeDatetime
		} else if x.Type() == mysql.TypeTimestamp {
			typeCode = JSONTypeCodeTimestamp
		}
		buf = appendBinaryUint64(buf, uint64(x.CoreTime()))
	case Duration:
		typeCode = JSONTypeCodeDuration
		buf = appendBinaryUint64(buf, uint64(x.Duration))
		buf = appendBinaryUint32(buf, uint32(x.Fsp))
	default:
		msg := fmt.Sprintf(unknownTypeErrorMsg, reflect.TypeOf(in))
		err = errors.New(msg)
	}
	return typeCode, buf, err
}

func appendZero(buf []byte, length int) []byte {
	var tmp [8]byte
	rem := length % 8
	loop := length / 8
	for i := 0; i < loop; i++ {
		buf = append(buf, tmp[:]...)
	}
	for i := 0; i < rem; i++ {
		buf = append(buf, 0)
	}
	return buf
}

func appendBinaryUint64(buf []byte, v uint64) []byte {
	off := len(buf)
	buf = appendZero(buf, 8)
	jsonEndian.PutUint64(buf[off:], v)
	return buf
}

func appendBinaryFloat64(buf []byte, v float64) []byte {
	off := len(buf)
	buf = appendZero(buf, 8)
	jsonEndian.PutUint64(buf[off:], math.Float64bits(v))
	return buf
}

func appendBinaryNumber(buf []byte, x json.Number) (TpCode, []byte, error) {
	if strings.Contains(x.String(), "Ee.") {
		f64, err := x.Float64()
		if err != nil {
			return TpCodeFloat64, nil, errors.Trace(err)
		}
		return TpCodeFloat64, appendBinaryFloat64(buf, f64), nil
	} else if val, err := x.Int64(); err == nil {
		return TpCodeFloat64, appendBinaryUint64(buf, uint64(val)), nil
	} else if val, err := strconv.ParseUint(string(x), 10, 64); err == nil {
		return TpCodeFloat64, appendBinaryUint64(buf, val), nil
	}
	val, err := x.Float64()
	if err == nil {
		return TpCodeFloat64, appendBinaryFloat64(buf, val), nil
	}
	var typeCode TpCode
	return typeCode, nil, errors.Trace(err)
}

func appendBinaryString(buf []byte, v string) []byte {
	begin := len(buf)
	buf = appendZero(buf, binary.MaxVarintLen64)
	lenLen := binary.PutUvarint(buf[begin:], uint64(len(v)))
	buf = buf[:len(buf)-binary.MaxVarintLen64+lenLen]
	buf = append(buf, v...)
	return buf
}

func appendUint32(buf []byte, v uint32) []byte {
	var tmp [4]byte
	jsonEndian.PutUint32(tmp[:], v)
	return append(buf, tmp[:]...)
}

func appendBinaryValElem(buf []byte, docOff, valEntryOff int, val any) ([]byte, error) {
	var typeCode TpCode
	var err error
	elemDocOff := len(buf)
	typeCode, buf, err = appendByteJSON(buf, val)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if typeCode == TpCodeLiteral {
		litCode := buf[elemDocOff]
		buf = buf[:elemDocOff]
		buf[valEntryOff] = TpCodeLiteral
		buf[valEntryOff+1] = litCode
		return buf, nil
	}
	buf[valEntryOff] = typeCode
	valOff := elemDocOff - docOff
	jsonEndian.PutUint32(buf[valEntryOff+1:], uint32(valOff))
	return buf, nil
}

func appendBinaryArray(buf []byte, array []any) ([]byte, error) {
	docOff := len(buf)
	buf = appendUint32(buf, uint32(len(array)))
	buf = appendZero(buf, dataSizeOff)
	valEntryBegin := len(buf)
	buf = appendZero(buf, len(array)*valEntrySize)
	for i, val := range array {
		var err error
		buf, err = appendBinaryValElem(buf, docOff, valEntryBegin+i*valEntrySize, val)
		if err != nil {
			return nil, errors.Trace(err)
		}
	}
	docSize := len(buf) - docOff
	jsonEndian.PutUint32(buf[docOff+dataSizeOff:], uint32(docSize))
	return buf, nil
}

func appendBinaryObject(buf []byte, x map[string]any) ([]byte, error) {
	docOff := len(buf)
	buf = appendUint32(buf, uint32(len(x)))
	buf = appendZero(buf, dataSizeOff)
	keyEntryBegin := len(buf)
	buf = appendZero(buf, len(x)*keyEntrySize)
	valEntryBegin := len(buf)
	buf = appendZero(buf, len(x)*valEntrySize)

	fields := make([]field, 0, len(x))
	for key, val := range x {
		fields = append(fields, field{key: key, val: val})
	}
	slices.SortFunc(fields, func(i, j field) int {
		return cmp.Compare(i.key, j.key)
	})
	for i, field := range fields {
		keyEntryOff := keyEntryBegin + i*keyEntrySize
		keyOff := len(buf) - docOff
		keyLen := uint32(len(field.key))
		if keyLen > math.MaxUint16 {
			return nil, nil
		}
		jsonEndian.PutUint32(buf[keyEntryOff:], uint32(keyOff))
		jsonEndian.PutUint16(buf[keyEntryOff+keyLenOff:], uint16(keyLen))
		buf = append(buf, field.key...)
	}
	for i, field := range fields {
		var err error
		buf, err = appendBinaryValElem(buf, docOff, valEntryBegin+i*valEntrySize, field.val)
		if err != nil {
			return nil, errors.Trace(err)
		}
	}
	docSize := len(buf) - docOff
	jsonEndian.PutUint32(buf[docOff+dataSizeOff:], uint32(docSize))
	return buf, nil
}
