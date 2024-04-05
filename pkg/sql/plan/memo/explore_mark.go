// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package memo

// ExploreMark is uses to mark whether a Group or GroupExpr has
// been fully explored by a transformation rule batch.
type ExploreMark int

// SetExplored sets the roundth bit.
func (m *ExploreMark) SetExplored(round int) {
	*m |= 1 << round
}

// SetUnexplored unsets the roundth bit.
func (m *ExploreMark) SetUnexplored(round int) {
	*m &= ^(1 << round)
}

// Explored returns whether the roundth bit has been set.
func (m *ExploreMark) Explored(round int) bool {
	return *m&(1<<round) != 0
}
