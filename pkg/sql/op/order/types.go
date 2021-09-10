package order

import (
	"matrixone/pkg/container/types"
	"matrixone/pkg/sql/op"
)

// Direction for ordering results.
type Direction int8

// Direction values.
const (
	DefaultDirection Direction = iota
	Ascending
	Descending
)

type Attribute struct {
	Name string
	Type types.T
	Dirt Direction
}

type Order struct {
	Prev op.OP
	IsPD bool // can be push down?
	ID   string
	Gs   []Attribute
}
