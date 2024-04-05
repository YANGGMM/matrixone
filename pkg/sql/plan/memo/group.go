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

package memo

import (
	"container/list"

	"github.com/matrixorigin/matrixone/pkg/sql/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/plan/pattern"
)

// Group is short for expression Group, which is used to store all the
// logically equivalent expressions. It's a set of GroupExpr.
type Group struct {
	Equivalents *list.List

	FirstExpr    map[pattern.Operand]*list.Element
	Fingerprints map[string]*list.Element

	//ImplMap map[string]Implementation
	//Prop    *property.LogicalProperty

	//EngineType pattern.EngineType

	SelfFingerprint string

	// ExploreMark is uses to mark whether this Group has been explored
	// by a transformation rule batch in a certain round.
	ExploreMark

	// hasBuiltKeyInfo indicates whether this group has called `BuildKeyInfo`.
	// BuildKeyInfo is lazily called when a rule needs information of
	// unique key or maxOneRow (in LogicalProp). For each Group, we only need
	// to collect these information once.
	hasBuiltKeyInfo bool
}

// NewGroupWithSchema creates a new Group with given schema.
// func NewGroupWithSchema(e *GroupExpr, s *expression.Schema) *Group {
// 	prop := &property.LogicalProperty{Schema: expression.NewSchema(s.Columns...)}
// 	g := &Group{
// 		Equivalents:  list.New(),
// 		Fingerprints: make(map[string]*list.Element),
// 		FirstExpr:    make(map[pattern.Operand]*list.Element),
// 		ImplMap:      make(map[string]Implementation),
// 		Prop:         prop,
// 		EngineType:   pattern.EngineTiDB,
// 	}
// 	g.Insert(e)
// 	return g
// }

func NewGroup(e *GroupExpr) *Group {
	g := &Group{
		Equivalents:  list.New(),
		Fingerprints: make(map[string]*list.Element),
		FirstExpr:    make(map[pattern.Operand]*list.Element),
	}
	g.Insert(e)
	return g
}

// Insert a nonexistent Group expression.
func (g *Group) Insert(e *GroupExpr) bool {
	if e == nil || g.Exists(e) {
		return false
	}

	operand := pattern.GetOperand(e.ExprNode)
	var newEquiv *list.Element
	mark, hasMark := g.FirstExpr[operand]
	if hasMark {
		newEquiv = g.Equivalents.InsertAfter(e, mark)
	} else {
		newEquiv = g.Equivalents.PushBack(e)
		g.FirstExpr[operand] = newEquiv
	}
	g.Fingerprints[e.FingerPrint()] = newEquiv
	e.Group = g
	return true
}

// Exists checks whether a Group expression existed in a Group.
func (g *Group) Exists(e *GroupExpr) bool {
	_, ok := g.Fingerprints[e.FingerPrint()]
	return ok
}

// Convert2GroupExpr converts a logical plan to a GroupExpr.
func Convert2GroupExpr(node *plan.Node) *GroupExpr {
	e := NewGroupExpr(node)
	e.Children = make([]*Group, 0, len(node.Children))

	for _, child := range node.Children {

		childGroup := Convert2Group(e.Builder.GetQuery().Nodes[child])
		e.Children = append(e.Children, childGroup)
	}
	return e
}

// Convert2Group converts a logical plan to a Group.
func Convert2Group(node *plan.Node) *Group {
	e := Convert2GroupExpr(node)
	g := NewGroup(e)
	// Stats property for `Group` would be computed after exploration phase.
	return g
}

// GetFirstElem returns the first Group expression which matches the Operand.
// Return a nil pointer if there isn't.
func (g *Group) GetFirstElem(operand pattern.Operand) *list.Element {
	if operand == pattern.OperandAny {
		return g.Equivalents.Front()
	}
	return g.FirstExpr[operand]
}
