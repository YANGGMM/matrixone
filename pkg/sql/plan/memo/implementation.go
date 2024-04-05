package memo

import "github.com/matrixorigin/matrixone/pkg/sql/plan"

type Implementation interface {
	CalcCost(outCount float64, children ...Implementation) float64
	SetCost(cost float64)
	GetCost() float64
	GetPlan() *plan.Node

	// AttachChildren is used to attach children implementations and returns it self.
	AttachChildren(children ...Implementation) Implementation

	// GetCostLimit gets the costLimit for implementing the next childGroup.
	GetCostLimit(costLimit float64, children ...Implementation) float64
}
