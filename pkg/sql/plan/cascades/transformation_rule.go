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

type Transformation interface {
	// GetPattern gets the cached pattern of the rule.
	GetPattern() *pattern.Pattern
	// Match is used to check whether the GroupExpr satisfies all the requirements of the transformation rule.
	//
	// The pattern only identifies the operator type, some transformation rules also need
	// detailed information for certain plan operators to decide whether it is applicable.
	Match(expr *memo.ExprIter) bool
	// OnTransform does the real work of the optimization rule.
	//
	// newExprs indicates the new GroupExprs generated by the transformationrule. Multiple GroupExprs may be
	// returned, e.g, EnumeratePath would convert DataSource to several possible assess paths.
	//
	// eraseOld indicates that the returned GroupExpr must be better than the old one, so we can remove it from Group.
	//
	// eraseAll indicates that the returned GroupExpr must be better than all other candidates in the Group, e.g, we can
	// prune all other access paths if we found the filter is constantly false.
	OnTransform(old *memo.ExprIter) (newExprs []*memo.GroupExpr, eraseOld bool, eraseAll bool, err error)
}

// TransformationRuleBatch is a batch of transformation rules.
type TransformationRuleBatch map[pattern.Operand][]Transformation

type baseRule struct {
	pattern *pattern.Pattern
}

// Match implements Transformation Interface.
func (*baseRule) Match(_ *memo.ExprIter) bool {
	return true
}

// GetPattern implements Transformation Interface.
func (r *baseRule) GetPattern() *pattern.Pattern {
	return r.pattern
}

// PushSelDownTableScan pushes the selection down to TableScan.
type PushSelDownTableScan struct {
	baseRule
}

// NewRulePushSelDownTableScan creates a new Transformation PushSelDownTableScan.
// The pattern of this rule is: `Selection -> TableScan`
func NewRulePushSelDownTableScan() Transformation {
	rule := &PushSelDownTableScan{}
	ts := pattern.NewPattern(pattern.OperandTableScan)
	p := pattern.BuildPattern(pattern.OperandSelection, ts)
	rule.pattern = p
	return rule
}

// OnTransform implements Transformation interface.
//
// It transforms `sel -> ts` to one of the following new exprs:
// 1. `newSel -> newTS`
// 2. `newTS`
//
// Filters of the old `sel` operator are removed if they are used to calculate
// the key ranges of the `ts` operator.
func (*PushSelDownTableScan) OnTransform(old *memo.ExprIter) (newExprs []*memo.GroupExpr, eraseOld bool, eraseAll bool, err error) {
	// sel := old.GetExpr().ExprNode.(*plannercore.LogicalSelection)
	// ts := old.Children[0].GetExpr().ExprNode.(*plannercore.LogicalTableScan)
	// if ts.HandleCols == nil {
	// 	return nil, false, false, nil
	// }
	// accesses, remained := ranger.DetachCondsForColumn(ts.SCtx(), sel.Conditions, ts.HandleCols.GetCol(0))
	// if accesses == nil {
	// 	return nil, false, false, nil
	// }
	// newTblScan := plannercore.LogicalTableScan{
	// 	Source:      ts.Source,
	// 	HandleCols:  ts.HandleCols,
	// 	AccessConds: ts.AccessConds.Shallow(),
	// }.Init(ts.SCtx(), ts.QueryBlockOffset())
	// newTblScan.AccessConds = append(newTblScan.AccessConds, accesses...)
	// tblScanExpr := memo.NewGroupExpr(newTblScan)
	// if len(remained) == 0 {
	// 	// `sel -> ts` is transformed to `newTS`.
	// 	return []*memo.GroupExpr{tblScanExpr}, true, false, nil
	// }
	// schema := old.GetExpr().Group.Prop.Schema
	// tblScanGroup := memo.NewGroupWithSchema(tblScanExpr, schema)
	// newSel := plannercore.LogicalSelection{Conditions: remained}.Init(sel.SCtx(), sel.QueryBlockOffset())
	// selExpr := memo.NewGroupExpr(newSel)
	// selExpr.Children = append(selExpr.Children, tblScanGroup)
	// // `sel -> ts` is transformed to `newSel ->newTS`.
	// return []*memo.GroupExpr{selExpr}, true, false, nil
	return nil, false, false, nil
}

var DefaultRuleBatches = []TransformationRuleBatch{
	{
		pattern.OperandSelection: {
			NewRulePushSelDownTableScan(),
		},
	},
}
