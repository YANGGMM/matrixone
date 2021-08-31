package min

import (
	"matrixone/pkg/container/types"
	"matrixone/pkg/container/vector"
	"matrixone/pkg/encoding"
	"matrixone/pkg/sql/colexec/aggregation"
	"matrixone/pkg/vectorize/min"
	"matrixone/pkg/vm/mempool"
	"matrixone/pkg/vm/process"
)

func NewUint64(typ types.Type) *uint64Min {
	return &uint64Min{typ: typ}
}

func (a *uint64Min) Reset() {
	a.v = 0
	a.cnt = 0
}

func (a *uint64Min) Type() types.Type {
	return a.typ
}

func (a *uint64Min) Dup() aggregation.Aggregation {
	return &uint64Min{typ: a.typ}
}

func (a *uint64Min) Fill(sels []int64, vec *vector.Vector) error {
	if n := len(sels); n > 0 {
		v := min.Uint64MinSels(vec.Col.([]uint64), sels)
		if a.cnt == 0 || v < a.v {
			a.v = v
		}
		a.cnt += int64(n - vec.Nsp.FilterCount(sels))
	} else {
		v := min.Uint64Min(vec.Col.([]uint64))
		a.cnt += int64(vec.Length() - vec.Nsp.Length())
		if a.cnt == 0 || v < a.v {
			a.v = v
		}
	}
	return nil
}

func (a *uint64Min) Eval() interface{} {
	if a.cnt == 0 {
		return nil
	}
	return a.v
}

func (a *uint64Min) EvalCopy(proc *process.Process) (*vector.Vector, error) {
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
