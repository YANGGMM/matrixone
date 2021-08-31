package max

import (
	"matrixone/pkg/container/types"
	"matrixone/pkg/container/vector"
	"matrixone/pkg/encoding"
	"matrixone/pkg/sql/colexec/aggregation"
	"matrixone/pkg/vectorize/max"
	"matrixone/pkg/vm/mempool"
	"matrixone/pkg/vm/process"
)

func NewUint64(typ types.Type) *uint64Max {
	return &uint64Max{typ: typ}
}

func (a *uint64Max) Reset() {
	a.v = 0
	a.cnt = 0
}

func (a *uint64Max) Type() types.Type {
	return a.typ
}

func (a *uint64Max) Dup() aggregation.Aggregation {
	return &uint64Max{typ: a.typ}
}

func (a *uint64Max) Fill(sels []int64, vec *vector.Vector) error {
	if n := len(sels); n > 0 {
		v := max.Uint64MaxSels(vec.Col.([]uint64), sels)
		if a.cnt == 0 || v > a.v {
			a.v = v
		}
		a.cnt += int64(n - vec.Nsp.FilterCount(sels))
	} else {
		v := max.Uint64Max(vec.Col.([]uint64))
		a.cnt += int64(vec.Length() - vec.Nsp.Length())
		if a.cnt == 0 || v > a.v {
			a.v = v
		}
	}
	return nil
}

func (a *uint64Max) Eval() interface{} {
	if a.cnt == 0 {
		return nil
	}
	return a.v
}

func (a *uint64Max) EvalCopy(proc *process.Process) (*vector.Vector, error) {
	data, err := proc.Alloc(8)
	if err != nil {
		return nil, err
	}
	vec := vector.New(a.typ)
	if a.cnt == 0 {
		vec.Nsp.Add(0)
		vs := []uint64{0}
		copy(data[mempool.CountSize:], encoding.EncodeUint64Slice(vs))
		vec.Col = vs
	} else {
		vs := []uint64{a.v}
		copy(data[mempool.CountSize:], encoding.EncodeUint64Slice(vs))
		vec.Col = vs
	}
	vec.Data = data
	return vec, nil
}
