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
package cascades

import (
	"github.com/matrixorigin/matrixone/pkg/sql/plan/memo"
	"github.com/matrixorigin/matrixone/pkg/sql/plan/pattern"
)

type ImplementationRule interface {
	// Match checks if current GroupExpr matches this rule under required physical property.
	Match(expr *memo.GroupExpr) (matched bool)
	// OnImplement generates physical plan using this rule for current GroupExpr. Note that
	// childrenReqProps of generated physical plan should be set correspondingly in this function.
	OnImplement(expr *memo.GroupExpr) ([]memo.Implementation, error)
}

var defaultImplementationMap = map[pattern.Operand][]ImplementationRule{}
