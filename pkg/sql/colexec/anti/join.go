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

package anti

import (
	"bytes"

	"github.com/matrixorigin/matrixone/pkg/common/hashmap"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/sql/colexec"
	"github.com/matrixorigin/matrixone/pkg/sql/plan"
	"github.com/matrixorigin/matrixone/pkg/vm/process"
)

func String(_ any, buf *bytes.Buffer) {
	buf.WriteString(" anti join ")
}

func Prepare(proc *process.Process, arg any) error {
	ap := arg.(*Argument)
	ap.ctr = new(container)
	ap.ctr.inBuckets = make([]uint8, hashmap.UnitLimit)
	ap.ctr.evecs = make([]evalVector, len(ap.Conditions[0]))
	ap.ctr.vecs = make([]*vector.Vector, len(ap.Conditions[0]))
	return nil
}

func Call(idx int, proc *process.Process, arg any) (bool, error) {
	anal := proc.GetAnalyze(idx)
	anal.Start()
	defer anal.Stop()
	ap := arg.(*Argument)
	ctr := ap.ctr
	for {
		switch ctr.state {
		case Build:
			if err := ctr.build(ap, proc, anal); err != nil {
				ctr.state = End
				if ctr.mp != nil {
					ctr.mp.Free()
				}
				return true, err
			}
			ctr.state = Probe
		case Probe:
			bat := <-proc.Reg.MergeReceivers[0].Ch
			if bat == nil {
				ctr.state = End
				if ctr.mp != nil {
					ctr.mp.Free()
				}
				if ctr.bat != nil {
					ctr.bat.Clean(proc.GetMheap())
				}
				continue
			}
			if bat.Length() == 0 {
				continue
			}
			if ctr.bat == nil || ctr.bat.Length() == 0 {
				if err := ctr.emptyProbe(bat, ap, proc, anal); err != nil {
					ctr.state = End
					if ctr.mp != nil {
						ctr.mp.Free()
					}
					proc.SetInputBatch(nil)
					return true, err
				}
			} else {
				if err := ctr.probe(bat, ap, proc, anal); err != nil {
					ctr.state = End
					if ctr.mp != nil {
						ctr.mp.Free()
					}
					proc.SetInputBatch(nil)
					return true, err
				}
			}
			return false, nil
		default:
			proc.SetInputBatch(nil)
			return true, nil
		}
	}
}

func (ctr *container) build(ap *Argument, proc *process.Process, anal process.Analyze) error {
	bat := <-proc.Reg.MergeReceivers[1].Ch
	if bat != nil {
		ctr.bat = bat
		ctr.mp = bat.Ht.(*hashmap.JoinMap).Dup()
		ctr.hasNull = ctr.mp.HasNull()
	}
	return nil
}

func (ctr *container) emptyProbe(bat *batch.Batch, ap *Argument, proc *process.Process, anal process.Analyze) error {
	defer bat.Clean(proc.GetMheap())
	anal.Input(bat)
	rbat := batch.NewWithSize(len(ap.Result))
	rbat.Zs = proc.GetMheap().GetSels()
	for i, pos := range ap.Result {
		rbat.Vecs[i] = vector.New(bat.Vecs[pos].Typ)
	}
	count := bat.Length()
	for i := 0; i < count; i += hashmap.UnitLimit {
		n := count - i
		if n > hashmap.UnitLimit {
			n = hashmap.UnitLimit
		}
		for k := 0; k < n; k++ {
			for j, pos := range ap.Result {
				if err := vector.UnionOne(rbat.Vecs[j], bat.Vecs[pos], int64(i+k), proc.GetMheap()); err != nil {
					rbat.Clean(proc.GetMheap())
					return err
				}
			}
			rbat.Zs = append(rbat.Zs, bat.Zs[i+k])
		}
	}
	rbat.ExpandNulls()
	anal.Output(rbat)
	proc.SetInputBatch(rbat)
	return nil
}

func (ctr *container) probe(bat *batch.Batch, ap *Argument, proc *process.Process, anal process.Analyze) error {
	defer bat.Clean(proc.Mp)
	anal.Input(bat)
	rbat := batch.NewWithSize(len(ap.Result))
	rbat.Zs = proc.GetMheap().GetSels()
	for i, pos := range ap.Result {
		rbat.Vecs[i] = vector.New(bat.Vecs[pos].Typ)
	}
	if (ctr.bat.Length() == 1 && ctr.hasNull) || ctr.bat.Length() == 0 {
		anal.Output(rbat)
		proc.SetInputBatch(rbat)
		return nil
	}
	if err := ctr.evalJoinCondition(bat, ap.Conditions[0], proc); err != nil {
		return err
	}
	defer ctr.freeJoinCondition(proc)
	count := bat.Length()
	mSels := ctr.mp.Sels()
	itr := ctr.mp.Map().NewIterator()
	for i := 0; i < count; i += hashmap.UnitLimit {
		n := count - i
		if n > hashmap.UnitLimit {
			n = hashmap.UnitLimit
		}
		copy(ctr.inBuckets, hashmap.OneUInt8s)
		vals, zvals := itr.Find(i, n, ctr.vecs, ctr.inBuckets)
		for k := 0; k < n; k++ {
			if ctr.inBuckets[k] == 0 || zvals[k] == 0 {
				continue
			}
			if vals[k] == 0 {
				for j, pos := range ap.Result {
					if err := vector.UnionOne(rbat.Vecs[j], bat.Vecs[pos], int64(i+k), proc.GetMheap()); err != nil {
						rbat.Clean(proc.GetMheap())
						return err
					}
				}
				rbat.Zs = append(rbat.Zs, bat.Zs[i+k])
				continue
			}
			if ap.Cond != nil {
				matched := false // mark if any tuple satisfies the condition
				sels := mSels[vals[k]-1]
				for _, sel := range sels {
					vec, err := colexec.JoinFilterEvalExprInBucket(bat, ctr.bat, i+k, int(sel), proc, ap.Cond)
					if err != nil {
						return err
					}
					bs := vec.Col.([]bool)
					if bs[0] {
						matched = true
						vec.Free(proc.Mp)
						break
					}
					vec.Free(proc.Mp)
				}
				if matched {
					continue
				}
				for j, pos := range ap.Result {
					if err := vector.UnionOne(rbat.Vecs[j], bat.Vecs[pos], int64(i+k), proc.GetMheap()); err != nil {
						rbat.Clean(proc.GetMheap())
						return err
					}
				}
				rbat.Zs = append(rbat.Zs, bat.Zs[i+k])
			}
		}
	}
	rbat.ExpandNulls()
	anal.Output(rbat)
	proc.SetInputBatch(rbat)
	return nil
}

func (ctr *container) evalJoinCondition(bat *batch.Batch, conds []*plan.Expr, proc *process.Process) error {
	for i, cond := range conds {
		vec, err := colexec.EvalExpr(bat, proc, cond)
		if err != nil || vec.ConstExpand(proc.GetMheap()) == nil {
			for j := 0; j < i; j++ {
				if ctr.evecs[j].needFree {
					vector.Clean(ctr.evecs[j].vec, proc.GetMheap())
				}
			}
			return err
		}
		ctr.vecs[i] = vec
		ctr.evecs[i].vec = vec
		ctr.evecs[i].needFree = true
		for j := range bat.Vecs {
			if bat.Vecs[j] == vec {
				ctr.evecs[i].needFree = false
				break
			}
		}
	}
	return nil
}

func (ctr *container) freeJoinCondition(proc *process.Process) {
	for i := range ctr.evecs {
		if ctr.evecs[i].needFree {
			ctr.evecs[i].vec.Free(proc.GetMheap())
		}
	}
}
