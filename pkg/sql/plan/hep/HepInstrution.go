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

package hep

// HepInstruction is an interface representing an instruction.
// In Java, this would be a reference to an instruction object.
type HepInstruction interface {
	PrepareContext() (HepPlanner, HepProgram)
}
type EndGroup struct {
	HepInstruction
}

type PrepareContext struct {
	planner       HepPlanner
	programState  interface{}
	endGroupState interface{}
}

// create 是一个函数，用于创建一个新的 PrepareContext 实例。
// 注意：在 Go 中，我们不使用 'static'，而是使用包级别的函数。
func create(planner HepPlanner, programState interface{}, endGroupState interface{}) *PrepareContext {
	return &PrepareContext{
		planner:       planner,
		programState:  programState,
		endGroupState: endGroupState,
	}
}

// withProgramState 是一个函数，用于返回一个新的 PrepareContext 实例，带有指定的程序状态。
func (ctx *PrepareContext) withProgramState(programState interface{}) *PrepareContext {
	return &PrepareContext{
		planner:       ctx.planner,
		programState:  programState,
		endGroupState: ctx.endGroupState,
	}
}

// withEndGroupState 是一个函数，用于返回一个新的 PrepareContext 实例，带有指定的结束组状态。
func (ctx *PrepareContext) withEndGroupState(endGroupState interface{}) *PrepareContext {
	return &PrepareContext{
		planner:       ctx.planner,
		programState:  ctx.programState,
		endGroupState: endGroupState,
	}
}
