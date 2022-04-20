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

package add

import (
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"math"
)

var (
	Int8Add                 func([]int8, []int8, []int8) []int8
	Int8AddSels             func([]int8, []int8, []int8, []int64) []int8
	Int8AddScalar           func(int8, []int8, []int8) []int8
	Int8AddScalarSels       func(int8, []int8, []int8, []int64) []int8
	Int16Add                func([]int16, []int16, []int16) []int16
	Int16AddSels            func([]int16, []int16, []int16, []int64) []int16
	Int16AddScalar          func(int16, []int16, []int16) []int16
	Int16AddScalarSels      func(int16, []int16, []int16, []int64) []int16
	Int32Add                func([]int32, []int32, []int32) []int32
	Int32AddSels            func([]int32, []int32, []int32, []int64) []int32
	Int32AddScalar          func(int32, []int32, []int32) []int32
	Int32AddScalarSels      func(int32, []int32, []int32, []int64) []int32
	Int64Add                func([]int64, []int64, []int64) []int64
	Int64AddSels            func([]int64, []int64, []int64, []int64) []int64
	Int64AddScalar          func(int64, []int64, []int64) []int64
	Int64AddScalarSels      func(int64, []int64, []int64, []int64) []int64
	Uint8Add                func([]uint8, []uint8, []uint8) []uint8
	Uint8AddSels            func([]uint8, []uint8, []uint8, []int64) []uint8
	Uint8AddScalar          func(uint8, []uint8, []uint8) []uint8
	Uint8AddScalarSels      func(uint8, []uint8, []uint8, []int64) []uint8
	Uint16Add               func([]uint16, []uint16, []uint16) []uint16
	Uint16AddSels           func([]uint16, []uint16, []uint16, []int64) []uint16
	Uint16AddScalar         func(uint16, []uint16, []uint16) []uint16
	Uint16AddScalarSels     func(uint16, []uint16, []uint16, []int64) []uint16
	Uint32Add               func([]uint32, []uint32, []uint32) []uint32
	Uint32AddSels           func([]uint32, []uint32, []uint32, []int64) []uint32
	Uint32AddScalar         func(uint32, []uint32, []uint32) []uint32
	Uint32AddScalarSels     func(uint32, []uint32, []uint32, []int64) []uint32
	Uint64Add               func([]uint64, []uint64, []uint64) []uint64
	Uint64AddSels           func([]uint64, []uint64, []uint64, []int64) []uint64
	Uint64AddScalar         func(uint64, []uint64, []uint64) []uint64
	Uint64AddScalarSels     func(uint64, []uint64, []uint64, []int64) []uint64
	Float32Add              func([]float32, []float32, []float32) []float32
	Float32AddSels          func([]float32, []float32, []float32, []int64) []float32
	Float32AddScalar        func(float32, []float32, []float32) []float32
	Float32AddScalarSels    func(float32, []float32, []float32, []int64) []float32
	Float64Add              func([]float64, []float64, []float64) []float64
	Float64AddSels          func([]float64, []float64, []float64, []int64) []float64
	Float64AddScalar        func(float64, []float64, []float64) []float64
	Float64AddScalarSels    func(float64, []float64, []float64, []int64) []float64
	Decimal64Add            func([]types.Decimal64, []types.Decimal64, int32, int32, []types.Decimal64) []types.Decimal64
	Decimal64AddSels        func([]types.Decimal64, []types.Decimal64, int32, int32, []types.Decimal64, []int64) []types.Decimal64
	Decimal64AddScalar      func(types.Decimal64, []types.Decimal64, int32, int32, []types.Decimal64) []types.Decimal64
	Decimal64AddScalarSels  func(types.Decimal64, []types.Decimal64, int32, int32, []types.Decimal64, []int64) []types.Decimal64
	Decimal128Add           func([]types.Decimal128, []types.Decimal128, int32, int32, []types.Decimal128) []types.Decimal128
	Decimal128AddSels       func([]types.Decimal128, []types.Decimal128, int32, int32, []types.Decimal128, []int64) []types.Decimal128
	Decimal128AddScalar     func(types.Decimal128, []types.Decimal128, int32, int32, []types.Decimal128) []types.Decimal128
	Decimal128AddScalarSels func(types.Decimal128, []types.Decimal128, int32, int32, []types.Decimal128, []int64) []types.Decimal128

	Int32Int64Add               func([]int32, []int64, []int64) []int64
	Int32Int64AddScalar         func(int32, []int64, []int64) []int64
	Int32Int64AddSels           func([]int32, []int64, []int64, []int64) []int64
	Int32Int64AddScalarSels     func(int32, []int64, []int64, []int64) []int64
	Int16Int64Add               func([]int16, []int64, []int64) []int64
	Int16Int64AddScalar         func(int16, []int64, []int64) []int64
	Int16Int64AddSels           func([]int16, []int64, []int64, []int64) []int64
	Int16Int64AddScalarSels     func(int16, []int64, []int64, []int64) []int64
	Int8Int64Add                func([]int8, []int64, []int64) []int64
	Int8Int64AddScalar          func(int8, []int64, []int64) []int64
	Int8Int64AddSels            func([]int8, []int64, []int64, []int64) []int64
	Int8Int64AddScalarSels      func(int8, []int64, []int64, []int64) []int64
	Int16Int32Add               func([]int16, []int32, []int32) []int32
	Int16Int32AddScalar         func(int16, []int32, []int32) []int32
	Int16Int32AddSels           func([]int16, []int32, []int32, []int64) []int32
	Int16Int32AddScalarSels     func(int16, []int32, []int32, []int64) []int32
	Int8Int32Add                func([]int8, []int32, []int32) []int32
	Int8Int32AddScalar          func(int8, []int32, []int32) []int32
	Int8Int32AddSels            func([]int8, []int32, []int32, []int64) []int32
	Int8Int32AddScalarSels      func(int8, []int32, []int32, []int64) []int32
	Int8Int16Add                func([]int8, []int16, []int16) []int16
	Int8Int16AddScalar          func(int8, []int16, []int16) []int16
	Int8Int16AddSels            func([]int8, []int16, []int16, []int64) []int16
	Int8Int16AddScalarSels      func(int8, []int16, []int16, []int64) []int16
	Float32Float64Add           func([]float32, []float64, []float64) []float64
	Float32Float64AddScalar     func(float32, []float64, []float64) []float64
	Float32Float64AddSels       func([]float32, []float64, []float64, []int64) []float64
	Float32Float64AddScalarSels func(float32, []float64, []float64, []int64) []float64
	Uint32Uint64Add             func([]uint32, []uint64, []uint64) []uint64
	Uint32Uint64AddScalar       func(uint32, []uint64, []uint64) []uint64
	Uint32Uint64AddSels         func([]uint32, []uint64, []uint64, []int64) []uint64
	Uint32Uint64AddScalarSels   func(uint32, []uint64, []uint64, []int64) []uint64
	Uint16Uint64Add             func([]uint16, []uint64, []uint64) []uint64
	Uint16Uint64AddScalar       func(uint16, []uint64, []uint64) []uint64
	Uint16Uint64AddSels         func([]uint16, []uint64, []uint64, []int64) []uint64
	Uint16Uint64AddScalarSels   func(uint16, []uint64, []uint64, []int64) []uint64
	Uint8Uint64Add              func([]uint8, []uint64, []uint64) []uint64
	Uint8Uint64AddScalar        func(uint8, []uint64, []uint64) []uint64
	Uint8Uint64AddSels          func([]uint8, []uint64, []uint64, []int64) []uint64
	Uint8Uint64AddScalarSels    func(uint8, []uint64, []uint64, []int64) []uint64
	Uint16Uint32Add             func([]uint16, []uint32, []uint32) []uint32
	Uint16Uint32AddScalar       func(uint16, []uint32, []uint32) []uint32
	Uint16Uint32AddSels         func([]uint16, []uint32, []uint32, []int64) []uint32
	Uint16Uint32AddScalarSels   func(uint16, []uint32, []uint32, []int64) []uint32
	Uint8Uint32Add              func([]uint8, []uint32, []uint32) []uint32
	Uint8Uint32AddScalar        func(uint8, []uint32, []uint32) []uint32
	Uint8Uint32AddSels          func([]uint8, []uint32, []uint32, []int64) []uint32
	Uint8Uint32AddScalarSels    func(uint8, []uint32, []uint32, []int64) []uint32
	Uint8Uint16Add              func([]uint8, []uint16, []uint16) []uint16
	Uint8Uint16AddScalar        func(uint8, []uint16, []uint16) []uint16
	Uint8Uint16AddSels          func([]uint8, []uint16, []uint16, []int64) []uint16
	Uint8Uint16AddScalarSels    func(uint8, []uint16, []uint16, []int64) []uint16
)

func init() {

	Decimal64Add = decimal64Add
	Decimal64AddSels = decimal64AddSels
	Decimal64AddScalar = decimal64AddScalar
	Decimal64AddScalarSels = decimal64AddScalarSels
	Decimal128Add = decimal128Add
	Decimal128AddSels = decimal128AddSels
	Decimal128AddScalar = decimal128AddScalar
	Decimal128AddScalarSels = decimal128AddScalarSels
}

func int8Add(xs, ys, rs []int8) []int8 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func int8AddSels(xs, ys, rs []int8, sels []int64) []int8 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func int8AddScalar(x int8, ys, rs []int8) []int8 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func int8AddScalarSels(x int8, ys, rs []int8, sels []int64) []int8 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func int16Add(xs, ys, rs []int16) []int16 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func int16AddSels(xs, ys, rs []int16, sels []int64) []int16 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func int16AddScalar(x int16, ys, rs []int16) []int16 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func int16AddScalarSels(x int16, ys, rs []int16, sels []int64) []int16 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func int32Add(xs, ys, rs []int32) []int32 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func int32AddSels(xs, ys, rs []int32, sels []int64) []int32 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func int32AddScalar(x int32, ys, rs []int32) []int32 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func int32AddScalarSels(x int32, ys, rs []int32, sels []int64) []int32 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func int64Add(xs, ys, rs []int64) []int64 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func int64AddSels(xs, ys, rs []int64, sels []int64) []int64 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func int64AddScalar(x int64, ys, rs []int64) []int64 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func int64AddScalarSels(x int64, ys, rs []int64, sels []int64) []int64 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func uint8Add(xs, ys, rs []uint8) []uint8 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func uint8AddSels(xs, ys, rs []uint8, sels []int64) []uint8 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func uint8AddScalar(x uint8, ys, rs []uint8) []uint8 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func uint8AddScalarSels(x uint8, ys, rs []uint8, sels []int64) []uint8 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func uint16Add(xs, ys, rs []uint16) []uint16 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func uint16AddSels(xs, ys, rs []uint16, sels []int64) []uint16 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func uint16AddScalar(x uint16, ys, rs []uint16) []uint16 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func uint16AddScalarSels(x uint16, ys, rs []uint16, sels []int64) []uint16 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func uint32Add(xs, ys, rs []uint32) []uint32 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func uint32AddSels(xs, ys, rs []uint32, sels []int64) []uint32 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func uint32AddScalar(x uint32, ys, rs []uint32) []uint32 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func uint32AddScalarSels(x uint32, ys, rs []uint32, sels []int64) []uint32 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func uint64Add(xs, ys, rs []uint64) []uint64 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func uint64AddSels(xs, ys, rs []uint64, sels []int64) []uint64 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func uint64AddScalar(x uint64, ys, rs []uint64) []uint64 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func uint64AddScalarSels(x uint64, ys, rs []uint64, sels []int64) []uint64 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func float32Add(xs, ys, rs []float32) []float32 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func float32AddSels(xs, ys, rs []float32, sels []int64) []float32 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func float32AddScalar(x float32, ys, rs []float32) []float32 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func float32AddScalarSels(x float32, ys, rs []float32, sels []int64) []float32 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func float64Add(xs, ys, rs []float64) []float64 {
	for i, x := range xs {
		rs[i] = x + ys[i]
	}
	return rs
}

func float64AddSels(xs, ys, rs []float64, sels []int64) []float64 {
	for i, sel := range sels {
		rs[i] = xs[sel] + ys[sel]
	}
	return rs
}

func float64AddScalar(x float64, ys, rs []float64) []float64 {
	for i, y := range ys {
		rs[i] = x + y
	}
	return rs
}

func float64AddScalarSels(x float64, ys, rs []float64, sels []int64) []float64 {
	for i, sel := range sels {
		rs[i] = x + ys[sel]
	}
	return rs
}

func int32Int64Add(xs []int32, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(xs[i]) + ys[i]
	}
	return rs
}

func int32Int64AddScalar(x int32, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(x) + ys[i]
	}
	return rs
}

func int32Int64AddSels(xs []int32, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(xs[sel]) + ys[sel]
	}
	return rs
}

func int32Int64AddScalarSels(x int32, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(x) + ys[sel]
	}
	return rs
}

func int16Int64Add(xs []int16, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(xs[i]) + ys[i]
	}
	return rs
}

func int16Int64AddScalar(x int16, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(x) + ys[i]
	}
	return rs
}

func int16Int64AddSels(xs []int16, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(xs[sel]) + ys[sel]
	}
	return rs
}

func int16Int64AddScalarSels(x int16, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(x) + ys[sel]
	}
	return rs
}

func int8Int64Add(xs []int8, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(xs[i]) + ys[i]
	}
	return rs
}

func int8Int64AddScalar(x int8, ys, rs []int64) []int64 {
	for i := range rs {
		rs[i] = int64(x) + ys[i]
	}
	return rs
}

func int8Int64AddSels(xs []int8, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(xs[sel]) + ys[sel]
	}
	return rs
}

func int8Int64AddScalarSels(x int8, ys, rs, sels []int64) []int64 {
	for _, sel := range sels {
		rs[sel] = int64(x) + ys[sel]
	}
	return rs
}

func int16Int32Add(xs []int16, ys, rs []int32) []int32 {
	for i := range rs {
		rs[i] = int32(xs[i]) + ys[i]
	}
	return rs
}

func int16Int32AddScalar(x int16, ys, rs []int32) []int32 {
	for i := range rs {
		rs[i] = int32(x) + ys[i]
	}
	return rs
}

func int16Int32AddSels(xs []int16, ys, rs []int32, sels []int64) []int32 {
	for _, sel := range sels {
		rs[sel] = int32(xs[sel]) + ys[sel]
	}
	return rs
}

func int16Int32AddScalarSels(x int16, ys, rs []int32, sels []int64) []int32 {
	for _, sel := range sels {
		rs[sel] = int32(x) + ys[sel]
	}
	return rs
}

func int8Int32Add(xs []int8, ys, rs []int32) []int32 {
	for i := range rs {
		rs[i] = int32(xs[i]) + ys[i]
	}
	return rs
}

func int8Int32AddScalar(x int8, ys, rs []int32) []int32 {
	for i := range rs {
		rs[i] = int32(x) + ys[i]
	}
	return rs
}

func int8Int32AddSels(xs []int8, ys, rs []int32, sels []int64) []int32 {
	for _, sel := range sels {
		rs[sel] = int32(xs[sel]) + ys[sel]
	}
	return rs
}

func int8Int32AddScalarSels(x int8, ys, rs []int32, sels []int64) []int32 {
	for _, sel := range sels {
		rs[sel] = int32(x) + ys[sel]
	}
	return rs
}

func int8Int16Add(xs []int8, ys, rs []int16) []int16 {
	for i := range rs {
		rs[i] = int16(xs[i]) + ys[i]
	}
	return rs
}

func int8Int16AddScalar(x int8, ys, rs []int16) []int16 {
	for i := range rs {
		rs[i] = int16(x) + ys[i]
	}
	return rs
}

func int8Int16AddSels(xs []int8, ys, rs []int16, sels []int64) []int16 {
	for _, sel := range sels {
		rs[sel] = int16(xs[sel]) + ys[sel]
	}
	return rs
}

func int8Int16AddScalarSels(x int8, ys, rs []int16, sels []int64) []int16 {
	for _, sel := range sels {
		rs[sel] = int16(x) + ys[sel]
	}
	return rs
}

func float32Float64Add(xs []float32, ys, rs []float64) []float64 {
	for i := range rs {
		rs[i] = float64(xs[i]) + ys[i]
	}
	return rs
}

func float32Float64AddScalar(x float32, ys, rs []float64) []float64 {
	for i := range rs {
		rs[i] = float64(x) + ys[i]
	}
	return rs
}

func float32Float64AddSels(xs []float32, ys, rs []float64, sels []int64) []float64 {
	for _, sel := range sels {
		rs[sel] = float64(xs[sel]) + ys[sel]
	}
	return rs
}

func float32Float64AddScalarSels(x float32, ys, rs []float64, sels []int64) []float64 {
	for _, sel := range sels {
		rs[sel] = float64(x) + ys[sel]
	}
	return rs
}

func uint32Uint64Add(xs []uint32, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(xs[i]) + ys[i]
	}
	return rs
}

func uint32Uint64AddScalar(x uint32, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(x) + ys[i]
	}
	return rs
}

func uint32Uint64AddSels(xs []uint32, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(xs[sel]) + ys[sel]
	}
	return rs
}

func uint32Uint64AddScalarSels(x uint32, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(x) + ys[sel]
	}
	return rs
}

func uint16Uint64Add(xs []uint16, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(xs[i]) + ys[i]
	}
	return rs
}

func uint16Uint64AddScalar(x uint16, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(x) + ys[i]
	}
	return rs
}

func uint16Uint64AddSels(xs []uint16, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(xs[sel]) + ys[sel]
	}
	return rs
}

func uint16Uint64AddScalarSels(x uint16, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(x) + ys[sel]
	}
	return rs
}

func uint8Uint64Add(xs []uint8, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(xs[i]) + ys[i]
	}
	return rs
}

func uint8Uint64AddScalar(x uint8, ys, rs []uint64) []uint64 {
	for i := range rs {
		rs[i] = uint64(x) + ys[i]
	}
	return rs
}

func uint8Uint64AddSels(xs []uint8, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(xs[sel]) + ys[sel]
	}
	return rs
}

func uint8Uint64AddScalarSels(x uint8, ys, rs []uint64, sels []int64) []uint64 {
	for _, sel := range sels {
		rs[sel] = uint64(x) + ys[sel]
	}
	return rs
}

func uint16Uint32Add(xs []uint16, ys, rs []uint32) []uint32 {
	for i := range rs {
		rs[i] = uint32(xs[i]) + ys[i]
	}
	return rs
}

func uint16Uint32AddScalar(x uint16, ys, rs []uint32) []uint32 {
	for i := range rs {
		rs[i] = uint32(x) + ys[i]
	}
	return rs
}

func uint16Uint32AddSels(xs []uint16, ys, rs []uint32, sels []int64) []uint32 {
	for _, sel := range sels {
		rs[sel] = uint32(xs[sel]) + ys[sel]
	}
	return rs
}

func uint16Uint32AddScalarSels(x uint16, ys, rs []uint32, sels []int64) []uint32 {
	for _, sel := range sels {
		rs[sel] = uint32(x) + ys[sel]
	}
	return rs
}

func uint8Uint32Add(xs []uint8, ys, rs []uint32) []uint32 {
	for i := range rs {
		rs[i] = uint32(xs[i]) + ys[i]
	}
	return rs
}

func uint8Uint32AddScalar(x uint8, ys, rs []uint32) []uint32 {
	for i := range rs {
		rs[i] = uint32(x) + ys[i]
	}
	return rs
}

func uint8Uint32AddSels(xs []uint8, ys, rs []uint32, sels []int64) []uint32 {
	for _, sel := range sels {
		rs[sel] = uint32(xs[sel]) + ys[sel]
	}
	return rs
}

func uint8Uint32AddScalarSels(x uint8, ys, rs []uint32, sels []int64) []uint32 {
	for _, sel := range sels {
		rs[sel] = uint32(x) + ys[sel]
	}
	return rs
}

func uint8Uint16Add(xs []uint8, ys, rs []uint16) []uint16 {
	for i := range rs {
		rs[i] = uint16(xs[i]) + ys[i]
	}
	return rs
}

func uint8Uint16AddScalar(x uint8, ys, rs []uint16) []uint16 {
	for i := range rs {
		rs[i] = uint16(x) + ys[i]
	}
	return rs
}

func uint8Uint16AddSels(xs []uint8, ys, rs []uint16, sels []int64) []uint16 {
	for _, sel := range sels {
		rs[sel] = uint16(xs[sel]) + ys[sel]
	}
	return rs
}

func uint8Uint16AddScalarSels(x uint8, ys, rs []uint16, sels []int64) []uint16 {
	for _, sel := range sels {
		rs[sel] = uint16(x) + ys[sel]
	}
	return rs
}

func decimal64Add(xs []types.Decimal64, ys []types.Decimal64, xsScale int32, ysScale int32, rs []types.Decimal64) []types.Decimal64 {
	/* to add two decimal64 value, first we need to align them to the same scale(the maximum of the two)
																	Decimal(10, 5), Decimal(10, 6)
	value																321.4			123.5
	representation														32,140,000		123,500,000
	align to the same scale	by scale 32,140,000 by 10 					321,400,000		123,500,000
	add
	*/
	if xsScale > ysScale {
		ysScaled := make([]types.Decimal64, len(ys))
		scaleDiff := xsScale - ysScale
		scale := int64(math.Pow10(int(scaleDiff)))
		for i, y := range ys {
			ysScaled[i] = types.ScaleDecimal64(y, scale)
		}
		for i, x := range xs {
			rs[i] = types.Decimal64AddAligned(x, ysScaled[i])
		}
		return rs
	} else if xsScale < ysScale {
		xsScaled := make([]types.Decimal64, len(xs))
		scaleDiff := ysScale - xsScale
		scale := int64(math.Pow10(int(scaleDiff)))
		for i, x := range xs {
			xsScaled[i] = types.ScaleDecimal64(x, scale)
		}
		for i, y := range ys {
			rs[i] = types.Decimal64AddAligned(xsScaled[i], y)
		}
		return rs
	} else {
		for i, x := range xs {
			rs[i] = types.Decimal64AddAligned(x, ys[i])
		}
		return rs
	}
}

func decimal64AddSels(xs, ys []types.Decimal64, xsScale, ysScale int32, rs []types.Decimal64, sels []int64) []types.Decimal64 {
	for i, sel := range sels {
		rs[i] = types.Decimal64Add(xs[sel], ys[sel], xsScale, ysScale)
	}
	return rs
}

func decimal64AddScalar(x types.Decimal64, ys []types.Decimal64, xScale, ysScale int32, rs []types.Decimal64) []types.Decimal64 {
	if xScale > ysScale {
		ysScaled := make([]types.Decimal64, len(ys))
		scaleDiff := xScale - ysScale
		scale := int64(math.Pow10(int(scaleDiff)))
		for i, y := range ys {
			ysScaled[i] = types.ScaleDecimal64(y, scale)
		}
		for i, yScaled := range ysScaled {
			rs[i] = types.Decimal64AddAligned(x, yScaled)
		}
		return rs
	} else if xScale < ysScale {
		xScaled := x
		scaleDiff := ysScale - xScale
		scale := int64(math.Pow10(int(scaleDiff)))
		xScaled = types.ScaleDecimal64(x, scale)
		for i, y := range ys {
			rs[i] = types.Decimal64AddAligned(xScaled, y)
		}
		return rs
	} else {
		for i, y := range ys {
			rs[i] = types.Decimal64AddAligned(x, y)
		}
		return rs
	}
}

func decimal64AddScalarSels(x types.Decimal64, ys []types.Decimal64, xScale, ysScale int32, rs []types.Decimal64, sels []int64) []types.Decimal64 {
	for i, sel := range sels {
		rs[i] = types.Decimal64Add(x, ys[sel], xScale, ysScale)
	}
	return rs
}

func decimal128Add(xs []types.Decimal128, ys []types.Decimal128, xsScale int32, ysScale int32, rs []types.Decimal128) []types.Decimal128 {
	/* to add two decimal128 value, first we need to align them to the same scale(the maximum of the two)
																	Decimal(20, 5), Decimal(20, 6)
	value																321.4			123.5
	representation														32,140,000		123,500,000
	align to the same scale	by scale 12340000 by 10 321400000			321,400,000		123,500,000
	add

	*/
	if xsScale > ysScale {
		ysScaled := make([]types.Decimal128, len(ys))
		scaleDiff := xsScale - ysScale
		for i, y := range ys {
			ysScaled[i] = y
			// since the possible scale difference is (0, 38], and 10**38 can not fit in a int64, double loop is necessary
			for j := 0; j < int(scaleDiff); j++ {
				ysScaled[i] = types.ScaleDecimal128By10(ysScaled[i])
			}
		}
		for i, x := range xs {
			rs[i] = types.Decimal128AddAligned(x, ysScaled[i])
		}
		return rs
	} else if xsScale < ysScale {
		xsScaled := make([]types.Decimal128, len(xs))
		scaleDiff := ysScale - xsScale
		for i, x := range xs {
			xsScaled[i] = x
			// since the possible scale difference is (0, 38], and 10**38 can not fit in a int64, double loop is necessary
			for j := 0; j < int(scaleDiff); j++ {
				xsScaled[i] = types.ScaleDecimal128By10(xsScaled[i])
			}
		}
		for i, y := range ys {
			rs[i] = types.Decimal128AddAligned(xsScaled[i], y)
		}
		return rs
	} else {
		for i, x := range xs {
			rs[i] = types.Decimal128AddAligned(x, ys[i])
		}
		return rs
	}
}

func decimal128AddSels(xs, ys []types.Decimal128, xsScale, ysScale int32, rs []types.Decimal128, sels []int64) []types.Decimal128 {
	for i, sel := range sels {
		rs[i] = types.Decimal128Add(xs[sel], ys[sel], xsScale, ysScale)
	}
	return rs
}

func decimal128AddScalar(x types.Decimal128, ys []types.Decimal128, xScale, ysScale int32, rs []types.Decimal128) []types.Decimal128 {
	if xScale > ysScale {
		ysScaled := make([]types.Decimal128, len(ys))
		scaleDiff := xScale - ysScale
		for i, y := range ys {
			ysScaled[i] = y
			// since the possible scale difference is (0, 38], and 10**38 can not fit in a int64, double loop is necessary
			for j := 0; j < int(scaleDiff); j++ {
				ysScaled[i] = types.ScaleDecimal128By10(ysScaled[i])
			}
		}
		for i, yScaled := range ysScaled {
			rs[i] = types.Decimal128AddAligned(x, yScaled)
		}
		return rs
	} else if xScale < ysScale {
		xScaled := x
		scaleDiff := ysScale - xScale
		// since the possible scale difference is (0, 38], and 10**38 can not fit in a int64, double loop is necessary
		for i := 0; i < int(scaleDiff); i++ {
			xScaled = types.ScaleDecimal128By10(xScaled)
		}
		for i, y := range ys {
			rs[i] = types.Decimal128AddAligned(xScaled, y)
		}
		return rs
	} else {
		for i, y := range ys {
			rs[i] = types.Decimal128AddAligned(x, y)
		}
		return rs
	}
}

func decimal128AddScalarSels(x types.Decimal128, ys []types.Decimal128, xScale, ysScale int32, rs []types.Decimal128, sels []int64) []types.Decimal128 {
	for i, sel := range sels {
		rs[i] = types.Decimal128Add(x, ys[sel], xScale, ysScale)
	}
	return rs
}
