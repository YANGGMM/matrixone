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
	"context"

	"github.com/matrixorigin/matrixone/pkg/sql/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/plan/pattern"
)

// DefaultOptimizer is the optimizer which contains all of the default
// transformation and implementation rules.
var DefaultOptimizer = NewOptimizer()

// Optimizer is the struct for cascades optimizer.
type Optimizer struct {
	transformationRuleBatches []TransformationRuleBatch
	implementationRuleMap     map[pattern.Operand][]ImplementationRule
}

// NewOptimizer returns a cascades optimizer with default transformation
// rules and implementation rules.
func NewOptimizer() *Optimizer {
	return &Optimizer{
		transformationRuleBatches: DefaultRuleBatches,
		implementationRuleMap:     defaultImplementationMap,
	}
}

// ResetTransformationRules resets the transformationRuleBatches of the optimizer, and returns the optimizer.
func (opt *Optimizer) ResetTransformationRules(ruleBatches ...TransformationRuleBatch) *Optimizer {
	opt.transformationRuleBatches = ruleBatches
	return opt
}

// GetImplementationRules gets all the candidate implementation rules of the optimizer
// for the logical plan node.
func (opt *Optimizer) GetImplementationRules(node *plan.Node) []ImplementationRule {
	return opt.implementationRuleMap[pattern.GetOperand(node)]
}

// FindBestPlan is the optimization entrance of the cascades planner. The
// optimization is composed of 3 phases: preprocessing, exploration and implementation.
//
// ------------------------------------------------------------------------------
// Phase 1: Preprocessing
// ------------------------------------------------------------------------------
//
// The target of this phase is to preprocess the plan tree by some heuristic
// rules which should always be beneficial, for example Column Pruning.
//
// ------------------------------------------------------------------------------
// Phase 2: Exploration
// ------------------------------------------------------------------------------
//
// The target of this phase is to explore all the logically equivalent
// expressions by exploring all the equivalent group expressions of each group.
//
// At the very beginning, there is only one group expression in a Group. After
// applying some transformation rules on certain expressions of the Group, all
// the equivalent expressions are found and stored in the Group. This procedure
// can be regarded as searching for a weak connected component in a directed
// graph, where nodes are expressions and directed edges are the transformation
// rules.
//
// ------------------------------------------------------------------------------
// Phase 3: Implementation
// ------------------------------------------------------------------------------
//
// The target of this phase is to search the best physical plan for a Group
// which satisfies a certain required physical property.
//
// In this phase, we need to enumerate all the applicable implementation rules
// for each expression in each group under the required physical property. A
// memo structure is used for a group to reduce the repeated search on the same
// required physical property.
func (opt *Optimizer) FindBestPlan(sctx context.Context, nodeID int32, step int32) (newNode int32, cost float64, err error) {
	nodeID, err = opt.onPhasePreprocessing(sctx, nodeID, step)
	if err != nil {
		return 0, 0, err
	}

	// err = opt.onPhaseExploration(sctx, rootGroup)
	// if err != nil {
	// 	return nil, 0, err
	// }
	// p, cost, err = opt.onPhaseImplementation(sctx, rootGroup)
	// if err != nil {
	// 	return nil, 0, err
	// }
	// err = p.ResolveIndices()
	// return p, cost, err
	return 0, 0, nil
}

func (opt *Optimizer) onPhasePreprocessing(_ context.Context, nodeID int32, step int32) (int32, error) {
	var err error

	if err != nil {
		return 0, err
	}
	return 0, nil
}
