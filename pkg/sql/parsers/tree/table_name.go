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

package tree

type TableName struct {
	TableExpr
	objName
	AtTsCluase *AtTimeStampClause
}

func (tn TableName) Format(ctx *FmtCtx) {
	if tn.ExplicitCatalog {
		ctx.WriteString(string(tn.CatalogName))
		ctx.WriteByte('.')
	}
	if tn.ExplicitSchema {
		ctx.WriteString(string(tn.SchemaName))
		ctx.WriteByte('.')
	}
	ctx.WriteString(string(tn.ObjectName))

	if tn.AtTsCluase != nil {
		ctx.WriteByte(' ')
		tn.AtTsCluase.Format(ctx)
	}
}

func (tn *TableName) Name() Identifier {
	return tn.ObjectName
}

func (tn *TableName) Schema() Identifier {
	return tn.SchemaName
}

func (tn *TableName) Catalog() Identifier {
	return tn.CatalogName
}

var _ TableExpr = &TableName{}

// table name array
type TableNames []*TableName

func (node *TableNames) Format(ctx *FmtCtx) {
	prefix := ""
	for _, t := range *node {
		ctx.WriteString(prefix)
		t.Format(ctx)
		prefix = ", "
	}
}

func NewTableName(name Identifier, prefix ObjectNamePrefix, atTs *AtTimeStampClause) *TableName {
	return &TableName{
		objName: objName{
			ObjectName:       name,
			ObjectNamePrefix: prefix,
		},
		AtTsCluase: atTs,
	}
}

type AtTimeStampClause struct {
	TimeStampExpr *TimeStampExpr
	SnapshotName  Identifier
}

func (node *AtTimeStampClause) Format(ctx *FmtCtx) {
	ctx.WriteString("AT ")
	if node.TimeStampExpr != nil {
		node.TimeStampExpr.Format(ctx)
	}
	if node.SnapshotName != "" {
		ctx.WriteString(" SNAPSHOT ")
		node.SnapshotName.Format(ctx)
	}
}

type TimeStampExpr struct {
	Expr Expr
}

func (node *TimeStampExpr) Format(ctx *FmtCtx) {
	ctx.WriteString("TIMESTAMP ")
	node.Expr.Format(ctx)
}
