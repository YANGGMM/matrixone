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

package util

import (
	"math"

	"github.com/demdxx/gocast"
	"github.com/spaolacci/murmur3"
)

type Encryptor struct {
}

func NewEncryptor() *Encryptor {
	return &Encryptor{}
}

func (e *Encryptor) Encrypt(origin string) int32 {
	hasher := murmur3.New32()
	_, _ = hasher.Write([]byte(origin))
	return int32(hasher.Sum32() % math.MaxInt32)
}

type LocalBloomService struct {
	m, k, n   int32
	bitmap    []int32
	encryptor Encryptor
}

func NewLocalBloomFilter(m, k int32, encryptor *Encryptor) *LocalBloomService {
	return &LocalBloomService{
		m:         m,
		k:         k,
		n:         0,
		bitmap:    make([]int32, m/32+1),
		encryptor: *encryptor,
	}
}

func (l *LocalBloomService) getKEncrypted(origin string) []int32 {
	rets := make([]int32, 0)
	val := origin
	for i := 0; int32(i) < l.k; i++ {
		ret := l.encryptor.Encrypt(val)
		rets = append(rets, ret%l.m)
		if int32(i) == l.k-1 {
			break
		}

		val = gocast.ToString(ret)
	}
	return rets
}

func (l *LocalBloomService) Exist(origin string) bool {
	for _, offset := range l.getKEncrypted(origin) {
		index := offset >> 5
		bitoffset := offset & 31

		if l.bitmap[index]&(1<<bitoffset) == 0 {
			return false
		}
	}
	return true
}

func (l *LocalBloomService) Set(val string) {
	l.n++
	for _, offset := range l.getKEncrypted(val) {
		index := offset >> 5     // 等价于 / 32
		bitOffset := offset & 31 // 等价于 % 32

		l.bitmap[index] |= (1 << bitOffset)
	}
}
