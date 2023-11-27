// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"context"
	"fmt"

	"github.com/matrixorigin/matrixone/pkg/defines"

	"github.com/google/uuid"
	"github.com/matrixorigin/matrixone/pkg/sql/plan/function"

	"github.com/matrixorigin/matrixone/pkg/catalog"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/tree"
	"github.com/matrixorigin/matrixone/pkg/sql/util"
)

const derivedTableName = "_t"
const maxRowThenUnusePkFilterExpr = 1024

type dmlSelectInfo struct {
	typ string

	projectList    []*Expr
	tblInfo        *dmlTableInfo
	idx            int32
	rootId         int32
	derivedTableId int32

	onDuplicateIdx      []int32
	onDuplicateExpr     map[string]*Expr
	onDuplicateNeedAgg  bool //if table have pk & unique key, that will be true.
	onDuplicateIsIgnore bool
}

type dmlTableInfo struct {
	typ            string
	objRef         []*ObjectRef
	tableDefs      []*TableDef
	isClusterTable []bool
	haveConstraint bool
	isMulti        bool
	needAggFilter  bool
	updateKeys     []map[string]tree.Expr // This slice index correspond to tableDefs
	oldColPosMap   []map[string]int       // origin table values to their position in derived table
	newColPosMap   []map[string]int       // insert/update values to their position in derived table
	nameToIdx      map[string]int         // Mapping of table full path name to tableDefs index，such as： 'tpch.nation -> 0'
	idToName       map[uint64]string      // Mapping of tableId to full path name of table
	alias          map[string]int         // Mapping of table aliases to tableDefs array index,If there is no alias, replace it with the original name of the table
}

var constTextType *plan.Type

func init() {
	typ := types.T_text.ToType()
	constTextType = makePlan2Type(&typ)
}

func getAliasToName(ctx CompilerContext, expr tree.TableExpr, alias string, aliasMap map[string][2]string) {
	switch t := expr.(type) {
	case *tree.TableName:
		dbName := string(t.SchemaName)
		if dbName == "" {
			dbName = ctx.DefaultDatabase()
		}
		tblName := string(t.ObjectName)
		if alias != "" {
			aliasMap[alias] = [2]string{dbName, tblName}
		}
	case *tree.AliasedTableExpr:
		alias := string(t.As.Alias)
		getAliasToName(ctx, t.Expr, alias, aliasMap)
	case *tree.JoinTableExpr:
		getAliasToName(ctx, t.Left, alias, aliasMap)
		getAliasToName(ctx, t.Right, alias, aliasMap)
	}
}

func getUpdateTableInfo(ctx CompilerContext, stmt *tree.Update) (*dmlTableInfo, error) {
	tblInfo, err := getDmlTableInfo(ctx, stmt.Tables, stmt.With, nil, "update")
	if err != nil {
		return nil, err
	}

	//check update field and set updateKeys
	usedTbl := make(map[string]map[string]tree.Expr)
	allColumns := make(map[string]map[string]struct{})
	for alias, idx := range tblInfo.alias {
		allColumns[alias] = make(map[string]struct{})
		for _, col := range tblInfo.tableDefs[idx].Cols {
			allColumns[alias][col.Name] = struct{}{}
		}
	}

	appendToTbl := func(table, column string, expr tree.Expr) {
		if _, exists := usedTbl[table]; !exists {
			usedTbl[table] = make(map[string]tree.Expr)
		}
		usedTbl[table][column] = expr
	}

	for _, updateExpr := range stmt.Exprs {
		if len(updateExpr.Names) > 1 {
			return nil, moerr.NewNYI(ctx.GetContext(), "unsupport expr")
		}
		parts := updateExpr.Names[0]
		expr := updateExpr.Expr
		if parts.NumParts > 1 {
			colName := parts.Parts[0]
			tblName := parts.Parts[1]
			if _, tblExists := tblInfo.alias[tblName]; tblExists {
				if _, colExists := allColumns[tblName][colName]; colExists {
					appendToTbl(tblName, colName, expr)
				} else {
					return nil, moerr.NewInternalError(ctx.GetContext(), "column '%v' not found in table %s", colName, tblName)
				}
			} else {
				return nil, moerr.NewNoSuchTable(ctx.GetContext(), "", tblName)
			}
		} else {
			colName := parts.Parts[0]
			tblName := ""
			found := false
			for alias, colulmns := range allColumns {
				if _, colExists := colulmns[colName]; colExists {
					if tblName != "" {
						return nil, moerr.NewInternalError(ctx.GetContext(), "Column '%v' in field list is ambiguous", colName)
					}
					found = true
					appendToTbl(alias, colName, expr)
				}
			}
			if !found && stmt.With != nil {
				var str string
				for i, c := range stmt.With.CTEs {
					if i > 0 {
						str += ", "
					}
					str += string(c.Name.Alias)
				}
				return nil, moerr.NewInternalError(ctx.GetContext(), "column '%v' not found in table or the target table %s of the UPDATE is not updatable", colName, str)
			} else if !found {
				return nil, moerr.NewInternalError(ctx.GetContext(), "column '%v' not found in table %s", colName, tblName)
			}
		}
	}

	// remove unused table
	newTblInfo := &dmlTableInfo{
		nameToIdx:     make(map[string]int),
		idToName:      make(map[uint64]string),
		alias:         make(map[string]int),
		isMulti:       tblInfo.isMulti,
		needAggFilter: tblInfo.needAggFilter,
	}
	for alias, columns := range usedTbl {
		idx := tblInfo.alias[alias]
		tblDef := tblInfo.tableDefs[idx]
		newTblInfo.objRef = append(newTblInfo.objRef, tblInfo.objRef[idx])
		newTblInfo.tableDefs = append(newTblInfo.tableDefs, tblDef)
		newTblInfo.isClusterTable = append(newTblInfo.isClusterTable, tblInfo.isClusterTable[idx])
		newTblInfo.alias[alias] = len(newTblInfo.tableDefs) - 1
		newTblInfo.updateKeys = append(newTblInfo.updateKeys, columns)

		if !newTblInfo.haveConstraint {
			if len(tblDef.RefChildTbls) > 0 {
				newTblInfo.haveConstraint = true
			} else if len(tblDef.Fkeys) > 0 {
				newTblInfo.haveConstraint = true
			} else {
				for _, indexdef := range tblDef.Indexes {
					if indexdef.Unique {
						newTblInfo.haveConstraint = true
						break
					}
				}
			}
		}
	}
	for idx, ref := range newTblInfo.objRef {
		key := ref.SchemaName + "." + ref.ObjName
		newTblInfo.idToName[newTblInfo.tableDefs[idx].TblId] = key
		newTblInfo.nameToIdx[key] = idx
	}

	return newTblInfo, nil
}

func setTableExprToDmlTableInfo(ctx CompilerContext, tbl tree.TableExpr, tblInfo *dmlTableInfo, aliasMap map[string][2]string, withMap map[string]struct{}) error {
	var tblName, dbName, alias string

	if aliasTbl, ok := tbl.(*tree.AliasedTableExpr); ok {
		alias = string(aliasTbl.As.Alias)
		tbl = aliasTbl.Expr
	}

	if jionTbl, ok := tbl.(*tree.JoinTableExpr); ok {
		tblInfo.needAggFilter = true
		err := setTableExprToDmlTableInfo(ctx, jionTbl.Left, tblInfo, aliasMap, withMap)
		if err != nil {
			return err
		}
		if jionTbl.Right != nil {
			return setTableExprToDmlTableInfo(ctx, jionTbl.Right, tblInfo, aliasMap, withMap)
		}
		return nil
	}

	if baseTbl, ok := tbl.(*tree.TableName); ok {
		tblName = string(baseTbl.ObjectName)
		dbName = string(baseTbl.SchemaName)
	}

	if _, exist := withMap[tblName]; exist {
		return nil
	}

	if aliasNames, exist := aliasMap[tblName]; exist {
		alias = tblName // work in delete statement
		dbName = aliasNames[0]
		tblName = aliasNames[1]
	}

	if tblName == "" {
		return nil
	}

	if dbName == "" {
		dbName = ctx.DefaultDatabase()
	}

	obj, tableDef := ctx.Resolve(dbName, tblName)
	if tableDef == nil {
		return moerr.NewNoSuchTable(ctx.GetContext(), dbName, tblName)
	}

	if tableDef.TableType == catalog.SystemExternalRel {
		return moerr.NewInvalidInput(ctx.GetContext(), "cannot insert/update/delete from external table")
	} else if tableDef.TableType == catalog.SystemViewRel {
		return moerr.NewInvalidInput(ctx.GetContext(), "cannot insert/update/delete from view")
	} else if tableDef.TableType == catalog.SystemSequenceRel && ctx.GetContext().Value(defines.BgKey{}) == nil {
		return moerr.NewInvalidInput(ctx.GetContext(), "Cannot insert/update/delete from sequence")
	}

	var newCols []*ColDef
	for _, col := range tableDef.Cols {
		if col.Hidden && tblInfo.typ == "insert" {
			if col.Name == catalog.FakePrimaryKeyColName {
				// fake pk is auto increment, need to fill.
				// TODO(fagongzi): we need to use a separate tag to mark the columns
				// for these behaviors, instead of using column names, which needs to
				// be changed after 0.8
				newCols = append(newCols, col)
			}
		} else {
			newCols = append(newCols, col)
		}
	}
	tableDef.Cols = newCols

	isClusterTable := util.TableIsClusterTable(tableDef.GetTableType())
	if isClusterTable && ctx.GetAccountId() != catalog.System_Account {
		return moerr.NewInternalError(ctx.GetContext(), "only the sys account can insert/update/delete the cluster table")
	}

	if util.TableIsClusterTable(tableDef.GetTableType()) && ctx.GetAccountId() != catalog.System_Account {
		return moerr.NewInternalError(ctx.GetContext(), "only the sys account can insert/update/delete the cluster table %s", tableDef.GetName())
	}
	if obj.PubInfo != nil {
		return moerr.NewInternalError(ctx.GetContext(), "cannot insert/update/delete from public table")
	}

	if !tblInfo.haveConstraint {
		if len(tableDef.RefChildTbls) > 0 {
			tblInfo.haveConstraint = true
		} else if len(tableDef.Fkeys) > 0 {
			tblInfo.haveConstraint = true
		} else {
			for _, indexdef := range tableDef.Indexes {
				if indexdef.Unique {
					tblInfo.haveConstraint = true
					break
				}
			}
		}
	}

	nowIdx := len(tblInfo.tableDefs)
	tblInfo.isClusterTable = append(tblInfo.isClusterTable, isClusterTable)
	tblInfo.objRef = append(tblInfo.objRef, &ObjectRef{
		Obj:        int64(tableDef.TblId),
		SchemaName: dbName,
		ObjName:    tblName,
	})
	tblInfo.tableDefs = append(tblInfo.tableDefs, tableDef)
	key := dbName + "." + tblName
	tblInfo.nameToIdx[key] = nowIdx
	tblInfo.idToName[tableDef.TblId] = key
	if alias == "" {
		alias = tblName
	}
	tblInfo.alias[alias] = nowIdx

	return nil
}

func getDmlTableInfo(ctx CompilerContext, tableExprs tree.TableExprs, with *tree.With, aliasMap map[string][2]string, typ string) (*dmlTableInfo, error) {
	tblInfo := &dmlTableInfo{
		typ:       typ,
		nameToIdx: make(map[string]int),
		idToName:  make(map[uint64]string),
		alias:     make(map[string]int),
	}

	cteMap := make(map[string]struct{})
	if with != nil {
		for _, cte := range with.CTEs {
			cteMap[string(cte.Name.Alias)] = struct{}{}
		}
	}

	for _, tbl := range tableExprs {
		err := setTableExprToDmlTableInfo(ctx, tbl, tblInfo, aliasMap, cteMap)
		if err != nil {
			return nil, err
		}
	}
	tblInfo.isMulti = len(tblInfo.objRef) > 1
	tblInfo.needAggFilter = tblInfo.needAggFilter || tblInfo.isMulti

	return tblInfo, nil
}

func initInsertStmt(builder *QueryBuilder, bindCtx *BindContext, stmt *tree.Insert, info *dmlSelectInfo) (bool, map[int]int, bool, error) {
	var err error
	var insertColumns []string
	tableDef := info.tblInfo.tableDefs[0]
	tableObjRef := info.tblInfo.objRef[0]
	syntaxHasColumnNames := false
	colToIdx := make(map[string]int)
	oldColPosMap := make(map[string]int)
	tableDef.Name2ColIndex = make(map[string]int32)
	for i, col := range tableDef.Cols {
		colToIdx[col.Name] = i
		oldColPosMap[col.Name] = i
		tableDef.Name2ColIndex[col.Name] = int32(i)
	}
	info.tblInfo.oldColPosMap = append(info.tblInfo.oldColPosMap, oldColPosMap)
	info.tblInfo.newColPosMap = append(info.tblInfo.newColPosMap, oldColPosMap)

	checkInsertPkDup := true
	isInsertWithoutAutoPkCol := false
	pkPosInValues := make(map[int]int)

	if stmt.Columns == nil {
		for _, col := range tableDef.Cols {
			// Hide pk can not added, because this column is auto increment
			// column. This must be fill by auto increment
			if col.Name != catalog.FakePrimaryKeyColName {
				insertColumns = append(insertColumns, col.Name)
			}
		}
	} else {
		syntaxHasColumnNames = true
		for _, column := range stmt.Columns {
			colName := string(column)
			if _, ok := colToIdx[colName]; !ok {
				return false, nil, false, moerr.NewBadFieldError(builder.GetContext(), colName, tableDef.Name)
			}
			insertColumns = append(insertColumns, colName)
		}
	}

	var astSlt *tree.Select
	switch slt := stmt.Rows.Select.(type) {
	// rewrite 'insert into tbl values (1,1)' to 'insert into tbl select * from (values row(1,1))'
	case *tree.ValuesClause:
		isAllDefault := false
		if slt.Rows[0] == nil {
			isAllDefault = true
		}
		if isAllDefault {
			for j, row := range slt.Rows {
				if row != nil {
					return false, nil, false, moerr.NewWrongValueCountOnRow(builder.GetContext(), j+1)
				}
			}
		} else {
			colCount := len(insertColumns)
			for j, row := range slt.Rows {
				if len(row) != colCount {
					return false, nil, false, moerr.NewWrongValueCountOnRow(builder.GetContext(), j+1)
				}
			}
		}

		//example1:insert into a values ();
		//but it does not work at the case:
		//insert into a(a) values (); insert into a values (0),();
		if isAllDefault && syntaxHasColumnNames {
			return false, nil, false, moerr.NewInvalidInput(builder.GetContext(), "insert values does not match the number of columns")
		}
		checkInsertPkDup = len(slt.Rows) > 1
		if CNPrimaryCheck && len(slt.Rows) <= maxRowThenUnusePkFilterExpr {
			if len(tableDef.Pkey.Names) == 1 {
				for idx, name := range insertColumns {
					if name == tableDef.Pkey.PkeyColName {
						pkPosInValues[idx] = 0
						break
					}
				}
			} else {
				pkNameMap := make(map[string]int)
				for pkIdx, pkName := range tableDef.Pkey.Names {
					pkNameMap[pkName] = pkIdx
				}
				for idx, name := range insertColumns {
					if pkIdx, ok := pkNameMap[name]; ok {
						pkPosInValues[idx] = pkIdx
					}
				}
			}
			// one of pk cols is incr col and this col was not in values.
			// we can not use the values of other cols as filterExpr.
			if len(tableDef.Pkey.Names) != len(pkPosInValues) {
				pkPosInValues = make(map[int]int)
			}
		}

		err = buildValueScan(isAllDefault, info, builder, bindCtx, tableDef, slt, insertColumns, colToIdx, stmt.OnDuplicateUpdate)
		if err != nil {
			return false, nil, false, err
		}

	case *tree.SelectClause:
		astSlt = stmt.Rows

		subCtx := NewBindContext(builder, bindCtx)
		info.rootId, err = builder.buildSelect(astSlt, subCtx, false)
		if err != nil {
			return false, nil, false, err
		}

	case *tree.ParenSelect:
		astSlt = slt.Select

		subCtx := NewBindContext(builder, bindCtx)
		info.rootId, err = builder.buildSelect(astSlt, subCtx, false)
		if err != nil {
			return false, nil, false, err
		}

	default:
		return false, nil, false, moerr.NewInvalidInput(builder.GetContext(), "insert has unknown select statement")
	}

	err = builder.addBinding(info.rootId, tree.AliasClause{
		Alias: derivedTableName,
	}, bindCtx)
	if err != nil {
		return false, nil, false, err
	}

	lastNode := builder.qry.Nodes[info.rootId]
	if len(insertColumns) != len(lastNode.ProjectList) {
		return false, nil, false, moerr.NewInvalidInput(builder.GetContext(), "insert values does not match the number of columns")
	}

	tag := builder.qry.Nodes[info.rootId].BindingTags[0]
	info.derivedTableId = info.rootId
	oldProject := append([]*Expr{}, lastNode.ProjectList...)

	insertColToExpr := make(map[string]*Expr)
	for i, column := range insertColumns {
		colIdx := colToIdx[column]
		projExpr := &plan.Expr{
			Typ: oldProject[i].Typ,
			Expr: &plan.Expr_Col{
				Col: &plan.ColRef{
					RelPos: tag,
					ColPos: int32(i),
				},
			},
		}
		projExpr, err = forceCastExpr(builder.GetContext(), projExpr, tableDef.Cols[colIdx].Typ)
		if err != nil {
			return false, nil, false, err
		}
		insertColToExpr[column] = projExpr
	}

	// have tables : t1(a default 0, b int, pk(a,b)) ,  t2(j int,k int)
	// rewrite 'insert into t1 select * from t2' to
	// select 'select _t.j, _t.k from (select * from t2) _t(j,k)
	// --------
	// rewrite 'insert into t1(b) values (1)' to
	// select 'select 0, _t.column_0 from (select * from values (1)) _t(column_0)
	projectList := make([]*Expr, 0, len(tableDef.Cols))
	for _, col := range tableDef.Cols {
		if oldExpr, exists := insertColToExpr[col.Name]; exists {
			projectList = append(projectList, oldExpr)
		} else {
			defExpr, err := getDefaultExpr(builder.GetContext(), col)
			if err != nil {
				return false, nil, false, err
			}

			if col.Typ.AutoIncr && col.Name == tableDef.Pkey.PkeyColName {
				isInsertWithoutAutoPkCol = true
			}
			projectList = append(projectList, defExpr)
		}
	}

	// append ProjectNode
	projectCtx := NewBindContext(builder, bindCtx)
	lastTag := builder.genNewTag()
	info.rootId = builder.appendNode(&plan.Node{
		NodeType:    plan.Node_PROJECT,
		ProjectList: projectList,
		Children:    []int32{info.rootId},
		BindingTags: []int32{lastTag},
	}, projectCtx)

	info.projectList = make([]*Expr, 0, len(projectList))
	info.derivedTableId = info.rootId
	for i, e := range projectList {
		info.projectList = append(info.projectList, &plan.Expr{
			Typ: e.Typ,
			Expr: &plan.Expr_Col{
				Col: &plan.ColRef{
					RelPos: lastTag,
					ColPos: int32(i),
				},
			},
		})
	}
	info.idx = int32(len(info.projectList))

	// if insert with on duplicate key . need append a join node
	// create table t1 (a int primary key, b int unique key, c int);
	// insert into t1 values (1,1,3),(2,2,3) on duplicate key update a=a+1, b=b-2;
	// rewrite to : select _t.*, t1.a, t1.b，t1.c, t1.row_id from
	//				(select * from values (1,1,3),(2,2,3)) _t(a,b,c) left join t1 on _t.a=t1.a or _t.b=t1.b
	if len(stmt.OnDuplicateUpdate) > 0 {
		isIgnore := len(stmt.OnDuplicateUpdate) == 1 && stmt.OnDuplicateUpdate[0] == nil
		if isIgnore {
			stmt.OnDuplicateUpdate = nil
		}

		rightTableDef := DeepCopyTableDef(tableDef, true)
		rightObjRef := DeepCopyObjectRef(tableObjRef)
		uniqueCols := GetUniqueColAndIdxFromTableDef(rightTableDef)
		if rightTableDef.Pkey != nil && rightTableDef.Pkey.PkeyColName == catalog.CPrimaryKeyColName {
			//rightTableDef.Cols = append(rightTableDef.Cols, MakeHiddenColDefByName(catalog.CPrimaryKeyColName))
			rightTableDef.Cols = append(rightTableDef.Cols, rightTableDef.Pkey.CompPkeyCol)
		}
		if rightTableDef.ClusterBy != nil && util.JudgeIsCompositeClusterByColumn(rightTableDef.ClusterBy.Name) {
			//rightTableDef.Cols = append(rightTableDef.Cols, MakeHiddenColDefByName(rightTableDef.ClusterBy.Name))
			rightTableDef.Cols = append(rightTableDef.Cols, rightTableDef.ClusterBy.CompCbkeyCol)
		}
		rightTableDef.Cols = append(rightTableDef.Cols, MakeRowIdColDef())
		rightTableDef.Name2ColIndex = map[string]int32{}
		for i, col := range rightTableDef.Cols {
			rightTableDef.Name2ColIndex[col.Name] = int32(i)
		}

		// if table have unique columns, we do the rewrite. if not, do nothing(do not throw error)
		if len(uniqueCols) > 0 {

			joinCtx := NewBindContext(builder, bindCtx)
			rightCtx := NewBindContext(builder, joinCtx)
			rightId := builder.appendNode(&plan.Node{
				NodeType:    plan.Node_TABLE_SCAN,
				ObjRef:      rightObjRef,
				TableDef:    rightTableDef,
				BindingTags: []int32{builder.genNewTag()},
			}, rightCtx)
			rightTag := builder.qry.Nodes[rightId].BindingTags[0]
			baseNodeTag := builder.qry.Nodes[info.rootId].BindingTags[0]

			//get update cols
			updateCols := make(map[string]tree.Expr)
			for _, updateExpr := range stmt.OnDuplicateUpdate {
				col := updateExpr.Names[0].Parts[0]
				updateCols[col] = updateExpr.Expr
			}

			var defExpr *Expr
			idxs := make([]int32, len(rightTableDef.Cols))
			updateExprs := make(map[string]*Expr)
			for i, col := range rightTableDef.Cols {
				info.idx = info.idx + 1
				idxs[i] = info.idx
				if updateExpr, exists := updateCols[col.Name]; exists {
					binder := NewUpdateBinder(builder.GetContext(), nil, nil, rightTableDef.Cols)
					binder.builder = builder
					if _, ok := updateExpr.(*tree.DefaultVal); ok {
						defExpr, err = getDefaultExpr(builder.GetContext(), col)
						if err != nil {
							return false, nil, false, err
						}
					} else {
						defExpr, err = binder.BindExpr(updateExpr, 0, true)
						if err != nil {
							return false, nil, false, err
						}
					}
					defExpr, err = forceCastExpr(builder.GetContext(), defExpr, col.Typ)
					if err != nil {
						return false, nil, false, err
					}
					updateExprs[col.Name] = defExpr
				}
				info.projectList = append(info.projectList, &plan.Expr{
					Typ: col.Typ,
					Expr: &plan.Expr_Col{
						Col: &plan.ColRef{
							RelPos: rightTag,
							ColPos: int32(i),
						},
					},
				})
			}

			// get join condition
			var joinConds *Expr
			joinIdx := 0
			for _, uniqueColMap := range uniqueCols {
				var condExpr *Expr
				condIdx := int(0)
				for _, colIdx := range uniqueColMap {
					col := rightTableDef.Cols[colIdx]
					leftExpr := &Expr{
						Typ: col.Typ,
						Expr: &plan.Expr_Col{
							Col: &plan.ColRef{
								RelPos: baseNodeTag,
								ColPos: int32(colIdx),
							},
						},
					}
					rightExpr := &plan.Expr{
						Typ: col.Typ,
						Expr: &plan.Expr_Col{
							Col: &plan.ColRef{
								RelPos: rightTag,
								ColPos: int32(colIdx),
							},
						},
					}
					eqExpr, err := BindFuncExprImplByPlanExpr(builder.GetContext(), "=", []*Expr{leftExpr, rightExpr})
					if err != nil {
						return false, nil, false, err
					}
					if condIdx == 0 {
						condExpr = eqExpr
					} else {
						condExpr, err = BindFuncExprImplByPlanExpr(builder.GetContext(), "and", []*Expr{condExpr, eqExpr})
						if err != nil {
							return false, nil, false, err
						}
					}
					condIdx++
				}

				if joinIdx == 0 {
					joinConds = condExpr
				} else {
					joinConds, err = BindFuncExprImplByPlanExpr(builder.GetContext(), "or", []*Expr{joinConds, condExpr})
					if err != nil {
						return false, nil, false, err
					}
				}
				joinIdx++
			}

			//append join node
			leftCtx := builder.ctxByNode[info.rootId]
			err = joinCtx.mergeContexts(builder.GetContext(), leftCtx, rightCtx)
			if err != nil {
				return false, nil, false, err
			}
			newRootId := builder.appendNode(&plan.Node{
				NodeType: plan.Node_JOIN,
				Children: []int32{info.rootId, rightId},
				JoinType: plan.Node_LEFT,
				OnList:   []*Expr{joinConds},
			}, joinCtx)
			bindCtx.binder = NewTableBinder(builder, bindCtx)
			info.rootId = newRootId
			info.onDuplicateIdx = idxs
			info.onDuplicateExpr = updateExprs
			info.onDuplicateNeedAgg = len(uniqueCols) > 1
			info.onDuplicateIsIgnore = isIgnore

			// append ProjectNode
			info.rootId = builder.appendNode(&plan.Node{
				NodeType:    plan.Node_PROJECT,
				ProjectList: info.projectList,
				Children:    []int32{info.rootId},
				BindingTags: []int32{builder.genNewTag()},
			}, bindCtx)
			bindCtx.results = info.projectList
		}
	}

	return checkInsertPkDup, pkPosInValues, isInsertWithoutAutoPkCol, nil
}

func deleteToSelect(builder *QueryBuilder, bindCtx *BindContext, node *tree.Delete, haveConstraint bool, tblInfo *dmlTableInfo) (int32, error) {
	var selectList []tree.SelectExpr
	fromTables := &tree.From{}

	getResolveExpr := func(alias string) {
		var ret *tree.UnresolvedName
		if haveConstraint {
			defIdx := tblInfo.alias[alias]
			for _, col := range tblInfo.tableDefs[defIdx].Cols {
				ret, _ = tree.NewUnresolvedName(builder.GetContext(), alias, col.Name)
				selectList = append(selectList, tree.SelectExpr{
					Expr: ret,
				})
			}
		} else {
			defIdx := tblInfo.alias[alias]
			ret, _ = tree.NewUnresolvedName(builder.GetContext(), alias, catalog.Row_ID)
			selectList = append(selectList, tree.SelectExpr{
				Expr: ret,
			})
			pkName := getTablePriKeyName(tblInfo.tableDefs[defIdx].Pkey)
			if pkName != "" {
				ret, _ = tree.NewUnresolvedName(builder.GetContext(), alias, pkName)
				selectList = append(selectList, tree.SelectExpr{
					Expr: ret,
				})
			}
		}
	}

	for _, tbl := range node.Tables {
		if aliasTbl, ok := tbl.(*tree.AliasedTableExpr); ok {
			alias := string(aliasTbl.As.Alias)
			if alias != "" {
				getResolveExpr(alias)
			} else {
				astTbl := aliasTbl.Expr.(*tree.TableName)
				getResolveExpr(string(astTbl.ObjectName))
			}
		} else if astTbl, ok := tbl.(*tree.TableName); ok {
			getResolveExpr(string(astTbl.ObjectName))
		}
	}

	if node.TableRefs != nil {
		fromTables.Tables = node.TableRefs
	} else {
		fromTables.Tables = node.Tables
	}

	astSelect := &tree.Select{
		Select: &tree.SelectClause{
			Distinct: false,
			Exprs:    selectList,
			From:     fromTables,
			Where:    node.Where,
		},
		OrderBy: node.OrderBy,
		Limit:   node.Limit,
		With:    node.With,
	}
	// ftCtx := tree.NewFmtCtx(dialectType)
	// astSelect.Format(ftCtx)
	// sql := ftCtx.String()
	// fmt.Print(sql)

	return builder.buildSelect(astSelect, bindCtx, false)
}

func checkNotNull(ctx context.Context, expr *Expr, tableDef *TableDef, col *ColDef) error {
	isConstantNull := false
	if ef, ok := expr.Expr.(*plan.Expr_C); ok {
		isConstantNull = ef.C.Isnull
	}
	if !isConstantNull {
		return nil
	}
	if col.Default != nil && !col.Default.NullAbility {
		return moerr.NewConstraintViolation(ctx, fmt.Sprintf("Column '%s' cannot be null", col.Name))
	}

	// if col.NotNull {
	// 	return moerr.NewConstraintViolation(ctx, fmt.Sprintf("Column '%s' cannot be null", col.Name))
	// }

	// if (col.Primary && !col.Typ.AutoIncr) ||
	// 	(col.Default != nil && !col.Default.NullAbility) {
	// 	return moerr.NewConstraintViolation(ctx, fmt.Sprintf("Column '%s' cannot be null", col.Name))
	// }

	// if tableDef.Pkey != nil && len(tableDef.Pkey.Names) > 1 {
	// 	names := tableDef.Pkey.Names
	// 	for _, name := range names {
	// 		if name == col.Name {
	// 			return moerr.NewConstraintViolation(ctx, fmt.Sprintf("Column '%s' cannot be null", name))
	// 		}
	// 	}
	// }

	return nil
}

var ForceCastExpr = forceCastExpr

func forceCastExpr2(ctx context.Context, expr *Expr, t2 types.Type, targetType *plan.Expr) (*Expr, error) {
	if targetType.Typ.Id == 0 {
		return expr, nil
	}
	t1 := makeTypeByPlan2Expr(expr)
	if t1.Eq(t2) {
		return expr, nil
	}

	targetType.Typ.NotNullable = expr.Typ.NotNullable
	fGet, err := function.GetFunctionByName(ctx, "cast", []types.Type{t1, t2})
	if err != nil {
		return nil, err
	}
	return &plan.Expr{
		Expr: &plan.Expr_F{
			F: &plan.Function{
				Func: &ObjectRef{Obj: fGet.GetEncodedOverloadID(), ObjName: "cast"},
				Args: []*Expr{expr, targetType},
			},
		},
		Typ: targetType.Typ,
	}, nil
}

func forceCastExpr(ctx context.Context, expr *Expr, targetType *Type) (*Expr, error) {
	if targetType.Id == 0 {
		return expr, nil
	}
	t1, t2 := makeTypeByPlan2Expr(expr), makeTypeByPlan2Type(targetType)
	if t1.Eq(t2) {
		return expr, nil
	}

	targetType.NotNullable = expr.Typ.NotNullable
	fGet, err := function.GetFunctionByName(ctx, "cast", []types.Type{t1, t2})
	if err != nil {
		return nil, err
	}
	t := &plan.Expr{
		Typ: targetType,
		Expr: &plan.Expr_T{
			T: &plan.TargetType{
				Typ: targetType,
			},
		},
	}
	return &plan.Expr{
		Expr: &plan.Expr_F{
			F: &plan.Function{
				Func: &ObjectRef{Obj: fGet.GetEncodedOverloadID(), ObjName: "cast"},
				Args: []*Expr{expr, t},
			},
		},
		Typ: targetType,
	}, nil
}

func buildValueScan(
	isAllDefault bool,
	info *dmlSelectInfo,
	builder *QueryBuilder,
	bindCtx *BindContext,
	tableDef *TableDef,
	slt *tree.ValuesClause,
	updateColumns []string,
	colToIdx map[string]int,
	OnDuplicateUpdate tree.UpdateExprs,
) error {
	var err error

	proc := builder.compCtx.GetProcess()
	lastTag := builder.genNewTag()
	colCount := len(updateColumns)
	rowsetData := &plan.RowsetData{
		Cols: make([]*plan.ColData, colCount),
	}
	for i := 0; i < colCount; i++ {
		rowsetData.Cols[i] = new(plan.ColData)
	}
	valueScanTableDef := &plan.TableDef{
		TblId: 0,
		Name:  "",
		Cols:  make([]*plan.ColDef, colCount),
	}
	projectList := make([]*Expr, colCount)
	bat := batch.NewWithSize(len(updateColumns))

	for i, colName := range updateColumns {
		col := tableDef.Cols[colToIdx[colName]]
		colTyp := makeTypeByPlan2Type(col.Typ)
		vec := proc.GetVector(colTyp)
		bat.Vecs[i] = vec
		targetTyp := &plan.Expr{
			Typ: col.Typ,
			Expr: &plan.Expr_T{
				T: &plan.TargetType{
					Typ: col.Typ,
				},
			},
		}
		var defExpr *Expr
		if isAllDefault {
			defExpr, err := getDefaultExpr(builder.GetContext(), col)
			if err != nil {
				return err
			}
			defExpr, err = forceCastExpr2(builder.GetContext(), defExpr, colTyp, targetTyp)
			if err != nil {
				return err
			}
			for j := range slt.Rows {
				if err := vector.AppendBytes(vec, nil, true, proc.Mp()); err != nil {
					bat.Clean(proc.Mp())
					return err
				}
				rowsetData.Cols[i].Data = append(rowsetData.Cols[i].Data, &plan.RowsetExpr{
					Pos:    -1,
					RowPos: int32(j),
					Expr:   defExpr,
				})
			}
		} else {
			binder := NewDefaultBinder(builder.GetContext(), nil, nil, col.Typ, nil)
			binder.builder = builder
			for j, r := range slt.Rows {
				if nv, ok := r[i].(*tree.NumVal); ok {
					canInsert, err := util.SetInsertValue(proc, nv, vec)
					if err != nil {
						bat.Clean(proc.Mp())
						return err
					}
					if canInsert {
						continue
					}
				}

				if err := vector.AppendBytes(vec, nil, true, proc.Mp()); err != nil {
					bat.Clean(proc.Mp())
					return err
				}
				if _, ok := r[i].(*tree.DefaultVal); ok {
					defExpr, err = getDefaultExpr(builder.GetContext(), col)
					if err != nil {
						bat.Clean(proc.Mp())
						return err
					}
				} else if nv, ok := r[i].(*tree.ParamExpr); ok {
					rowsetData.Cols[i].Data = append(rowsetData.Cols[i].Data, &plan.RowsetExpr{
						RowPos: int32(j),
						Pos:    int32(nv.Offset),
						Expr: &plan.Expr{
							Typ: constTextType,
							Expr: &plan.Expr_P{
								P: &plan.ParamRef{
									Pos: int32(nv.Offset),
								},
							},
						},
					})
					continue
				} else {
					defExpr, err = binder.BindExpr(r[i], 0, true)
					if err != nil {
						bat.Clean(proc.Mp())
						return err
					}
					if col.Typ != nil && col.Typ.Id == int32(types.T_enum) {
						defExpr, err = funcCastForEnumType(builder.GetContext(), defExpr, col.GetTyp())
						if err != nil {
							bat.Clean(proc.Mp())
							return err
						}
					}
				}
				defExpr, err = forceCastExpr2(builder.GetContext(), defExpr, colTyp, targetTyp)
				if err != nil {
					return err
				}
				if nv, ok := r[i].(*tree.ParamExpr); ok {
					rowsetData.Cols[i].Data = append(rowsetData.Cols[i].Data, &plan.RowsetExpr{
						RowPos: int32(j),
						Pos:    int32(nv.Offset),
						Expr:   defExpr,
					})
					continue
				}
				rowsetData.Cols[i].Data = append(rowsetData.Cols[i].Data, &plan.RowsetExpr{
					Pos:    -1,
					RowPos: int32(j),
					Expr:   defExpr,
				})
			}
		}
		colName := fmt.Sprintf("column_%d", i) // like MySQL
		valueScanTableDef.Cols[i] = &plan.ColDef{
			ColId: 0,
			Name:  colName,
			Typ:   col.Typ,
		}
		expr := &plan.Expr{
			Typ: col.Typ,
			Expr: &plan.Expr_Col{
				Col: &plan.ColRef{
					RelPos: lastTag,
					ColPos: int32(i),
				},
			},
		}
		projectList[i] = expr
	}

	onUpdateExprs := make([]*plan.Expr, 0)
	for _, expr := range OnDuplicateUpdate {
		var updateExpr *plan.Expr
		col := tableDef.Cols[colToIdx[expr.Names[0].Parts[0]]]
		if nv, ok := expr.Expr.(*tree.ParamExpr); ok {
			updateExpr = &plan.Expr{
				Typ: constTextType,
				Expr: &plan.Expr_P{
					P: &plan.ParamRef{
						Pos: int32(nv.Offset),
					},
				},
			}
		} else if nv, ok := expr.Expr.(*tree.FuncExpr); ok {
			if checkExprHasParamExpr(nv.Exprs) {
				binder := NewDefaultBinder(builder.GetContext(), nil, nil, col.Typ, nil)
				binder.builder = builder
				binder.ctx = bindCtx
				updateExpr, err = binder.BindExpr(nv, 0, true)
				if err != nil {
					return err
				}
			}
		} else if nv, ok := expr.Expr.(*tree.BinaryExpr); ok {
			if checkExprHasParamExpr([]tree.Expr{nv.Right}) {
				binder := NewDefaultBinder(builder.GetContext(), nil, nil, col.Typ, nil)
				binder.builder = builder
				binder.ctx = bindCtx
				updateExpr, err = binder.BindExpr(nv.Right, 0, true)
				if err != nil {
					return err
				}
			}
		}
		if updateExpr != nil {
			onUpdateExprs = append(onUpdateExprs, updateExpr)
		}

	}
	bat.SetRowCount(len(slt.Rows))
	nodeId, _ := uuid.NewUUID()
	scanNode := &plan.Node{
		NodeType:      plan.Node_VALUE_SCAN,
		RowsetData:    rowsetData,
		TableDef:      valueScanTableDef,
		BindingTags:   []int32{lastTag},
		Uuid:          nodeId[:],
		OnUpdateExprs: onUpdateExprs,
	}
	if builder.isPrepareStatement {
		proc.SetPrepareBatch(bat)
	} else {
		proc.SetValueScanBatch(nodeId, bat)
	}
	info.rootId = builder.appendNode(scanNode, bindCtx)
	err = builder.addBinding(info.rootId, tree.AliasClause{
		Alias: "_ValueScan",
	}, bindCtx)
	if err != nil {
		return err
	}
	lastTag = builder.genNewTag()
	info.rootId = builder.appendNode(&plan.Node{
		NodeType:    plan.Node_PROJECT,
		ProjectList: projectList,
		Children:    []int32{info.rootId},
		BindingTags: []int32{lastTag},
	}, bindCtx)
	return nil
}
