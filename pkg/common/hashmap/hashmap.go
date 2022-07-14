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

package hashmap

import (
	"unsafe"

	"github.com/matrixorigin/matrixone/pkg/container/hashtable"
	"github.com/matrixorigin/matrixone/pkg/container/nulls"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
)

func New() *HashMap {
	mp := &hashtable.StringHashMap{}
	mp.Init()
	return &HashMap{
		hashMap:       mp,
		values:        make([]uint64, UnitLimit),
		keys:          make([][]byte, UnitLimit),
		strHashStates: make([][3]uint64, UnitLimit),
	}
}

// Inserts a row from multiple columns into the hashmap, return true if it is new data, false otherwise
func (m *HashMap) Insert(vecs []*vector.Vector, row int) bool {
	for _, vec := range vecs {
		switch typLen := vec.Typ.TypeSize(); typLen {
		case 1:
			fillGroupStr[uint8](m, vec, 1, 1, row)
		case 2:
			fillGroupStr[uint16](m, vec, 1, 2, row)
		case 4:
			fillGroupStr[uint32](m, vec, 1, 4, row)
		case 8:
			fillGroupStr[uint64](m, vec, 1, 8, row)
		case 16:
			fillGroupStr[types.Decimal128](m, vec, 1, 16, row)
		default:
			vs := vec.Col.(*types.Bytes)
			if !nulls.Any(vec.Nsp) {
				m.keys[0] = append(m.keys[0], vs.Get(int64(row))...)
			} else {
				if !nulls.Contains(vec.Nsp, uint64(row)) {
					m.keys[0] = append(m.keys[0], vs.Get(int64(row))...)
				}
			}
		}
	}
	if l := len(m.keys[0]); l < 16 {
		m.keys[0] = append(m.keys[0], hashtable.StrKeyPadding[l:]...)
	}
	m.hashMap.InsertStringBatch(m.strHashStates, m.keys[:1], m.values[:1])
	if m.values[0] > m.rows {
		m.rows++
		return true
	}
	return false
}

/*
func (m *HashMap) InsertBatch() {
}
*/

func fillGroupStr[T any](m *HashMap, vec *vector.Vector, n int, sz int, start int) {
	vs := vector.GetFixedVectorValues[T](vec, int(sz))
	data := unsafe.Slice((*byte)(unsafe.Pointer(&vs[0])), cap(vs)*sz)[:len(vs)*sz]
	if !nulls.Any(vec.Nsp) {
		for i := 0; i < n; i++ {
			m.keys[i] = append(m.keys[i], data[(i+start)*sz:(i+start+1)*sz]...)
		}
	} else {
		for i := 0; i < n; i++ {
			if !nulls.Contains(vec.Nsp, uint64(i+start)) {
				m.keys[i] = append(m.keys[i], data[(i+start)*sz:(i+start+1)*sz]...)
			}
		}
	}
}
