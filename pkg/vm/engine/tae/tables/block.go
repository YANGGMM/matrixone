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

package tables

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/compute"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/containers"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/model"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/wal"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tables/evictable"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tables/indexwrapper"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tables/jobs"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tables/updates"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tasks"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/buffer/base"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/catalog"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/data"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/file"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/handle"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
)

// The initial state of the block when scoring
type statBlock struct {
	rows      uint32
	startTime time.Time
}

type dataBlock struct {
	*sync.RWMutex
	common.RefHelper
	common.ClosedState
	meta         *catalog.BlockEntry
	node         *appendableNode
	file         file.Block
	colObjects   map[int]file.ColumnBlock
	bufMgr       base.INodeManager
	scheduler    tasks.TaskScheduler
	indexes      map[int]indexwrapper.Index
	pkIndex      indexwrapper.Index // a shortcut, nil if no pk column
	mvcc         *updates.MVCCHandle
	score        *statBlock
	ckpTs        atomic.Value
	appendFrozen bool
}

func newBlock(meta *catalog.BlockEntry, segFile file.Segment, bufMgr base.INodeManager, scheduler tasks.TaskScheduler) *dataBlock {
	colCnt := len(meta.GetSchema().ColDefs)
	schema := meta.GetSchema()
	blockFile, err := segFile.OpenBlock(meta.GetID(), colCnt)
	if err != nil {
		panic(err)
	}
	colObjects := make(map[int]file.ColumnBlock)
	for i := 0; i < colCnt; i++ {
		if colBlk, err := blockFile.OpenColumn(i); err != nil {
			panic(err)
		} else {
			colObjects[i] = colBlk
			colBlk.Close()
		}
	}
	var node *appendableNode
	//var zeroV types.TS
	block := &dataBlock{
		RWMutex:    new(sync.RWMutex),
		meta:       meta,
		file:       blockFile,
		colObjects: colObjects,
		mvcc:       updates.NewMVCCHandle(meta),
		scheduler:  scheduler,
		indexes:    make(map[int]indexwrapper.Index),
		bufMgr:     bufMgr,
	}
	block.SetMaxCheckpointTS(types.TS{})
	block.mvcc.SetAppendListener(block.OnApplyAppend)
	if meta.IsAppendable() {
		block.mvcc.SetDeletesListener(block.ABlkApplyDelete)
		node = newNode(bufMgr, block, blockFile)
		block.node = node
		// if this block is created to receive data, create mutable index first
		for _, colDef := range schema.ColDefs {
			if colDef.IsPhyAddr() {
				continue
			}
			if colDef.IsPrimary() {
				block.indexes[colDef.Idx] = indexwrapper.NewPkMutableIndex(colDef.Type)
				block.pkIndex = block.indexes[colDef.Idx]
			} else {
				block.indexes[colDef.Idx] = indexwrapper.NewMutableIndex(colDef.Type)
			}
		}
	} else {
		block.mvcc.SetDeletesListener(block.BlkApplyDelete)
		// if this block is created to do compact or merge, no need to new index
		// if this block is loaded from storage, ReplayIndex will create index
	}
	if meta.GetMetaLoc() != "" {
		if err := block.ReplayIndex(); err != nil {
			panic(err)
		}
		if err := block.ReplayDelta(); err != nil {
			panic(err)
		}
	}
	return block
}

func (blk *dataBlock) GetMeta() any                 { return blk.meta }
func (blk *dataBlock) GetBufMgr() base.INodeManager { return blk.bufMgr }
func (blk *dataBlock) FreezeAppend() {
	blk.Lock()
	defer blk.Unlock()
	blk.appendFrozen = true
}

func (blk *dataBlock) IsAppendFrozen() bool {
	blk.RLock()
	defer blk.RUnlock()
	return blk.appendFrozen
}

func (blk *dataBlock) SetMaxCheckpointTS(ts types.TS) {
	blk.ckpTs.Store(ts)
}

func (blk *dataBlock) GetMaxCheckpointTS() types.TS {
	ts := blk.ckpTs.Load().(types.TS)
	return ts
}

func (blk *dataBlock) FreeData() {
	blk.Lock()
	defer blk.Unlock()
	if blk.node != nil {
		_ = blk.node.Close()
	}
}

func (blk *dataBlock) Close() {
	if blk.node != nil {
		_ = blk.node.Close()
		blk.node = nil
	}
	if blk.file != nil {
		_ = blk.file.Close()
		blk.file = nil
	}
}

func (blk *dataBlock) Destroy() (err error) {
	if !blk.TryClose() {
		return
	}
	if blk.node != nil {
		if err = blk.node.Close(); err != nil {
			return
		}
	}
	for _, file := range blk.colObjects {
		file.Close()
	}
	blk.colObjects = make(map[int]file.ColumnBlock)
	for _, index := range blk.indexes {
		if err = index.Destroy(); err != nil {
			return
		}
	}
	if blk.file != nil {
		if err = blk.file.Close(); err != nil {
			return
		}
		if err = blk.file.Destroy(); err != nil {
			return
		}
	}
	return
}

func (blk *dataBlock) GetBlockFile() file.Block {
	return blk.file
}

func (blk *dataBlock) GetID() *common.ID { return blk.meta.AsCommonID() }

func (blk *dataBlock) RunCalibration() (score int) {
	score, _ = blk.estimateRawScore()
	return
}

func (blk *dataBlock) estimateABlkRawScore() (score int) {
	// Max row appended
	rows := blk.Rows()
	if rows == int(blk.meta.GetSchema().BlockMaxRows) {
		return 100
	}

	if blk.mvcc.GetChangeNodeCnt() == 0 && rows == 0 {
		// No deletes or append found
		score = 0
	} else {
		// Any deletes or append
		score = 1
	}
	return
}

func (blk *dataBlock) estimateRawScore() (score int, dropped bool) {
	if blk.meta.HasDropped() {
		dropped = true
		return
	}
	if blk.meta.IsAppendable() {
		score = blk.estimateABlkRawScore()
		return
	}
	if blk.mvcc.GetChangeNodeCnt() == 0 {
		// No deletes found
		score = 0
	} else {
		// Any delete
		score = 1
	}
	return
}

func (blk *dataBlock) MutationInfo() string {
	rows := blk.Rows()
	totalChanges := blk.mvcc.GetChangeNodeCnt()
	s := fmt.Sprintf("Block %s Mutation Info: Changes=%d/%d",
		blk.meta.AsCommonID().BlockString(),
		totalChanges,
		rows)
	if totalChanges == 0 {
		return s
	}
	deleteCnt := blk.mvcc.GetDeleteCnt()
	if deleteCnt != 0 {
		s = fmt.Sprintf("%s, Del:%d/%d", s, deleteCnt, rows)
	}
	return s
}

func (blk *dataBlock) EstimateScore(interval int64) int {
	score, dropped := blk.estimateRawScore()
	if dropped {
		return 0
	}
	if score > 1 {
		return score
	}
	rows := uint32(blk.Rows()) - blk.mvcc.GetDeleteCnt()
	if blk.score == nil {
		blk.score = &statBlock{
			rows:      rows,
			startTime: time.Now(),
		}
		return 1
	}
	if blk.score.rows != rows {
		// When the rows in the data block are modified, reset the statBlock
		blk.score.rows = rows
		blk.score.startTime = time.Now()
	} else {
		s := time.Since(blk.score.startTime).Milliseconds()
		if s > interval {
			return 100
		}
	}
	return 1
}

func (blk *dataBlock) BuildCompactionTaskFactory() (
	factory tasks.TxnTaskFactory,
	taskType tasks.TaskType,
	scopes []common.ID,
	err error) {
	// If the conditions are met, immediately modify the data block status to NotAppendable
	blk.FreezeAppend()
	blk.meta.RLock()
	dropped := blk.meta.IsDroppedCommitted()
	inTxn := blk.meta.IsCreating()
	blk.meta.RUnlock()
	if dropped || inTxn {
		return
	}
	// Make sure no appender use this block to compact
	if blk.RefCount() > 0 {
		// logutil.Infof("blk.RefCount() != 0 : %v, rows: %d", blk.meta.String(), blk.node.rows)
		return
	}
	//logutil.Infof("CompactBlockTaskFactory blk: %d, rows: %d", blk.meta.ID, blk.node.rows)
	factory = jobs.CompactBlockTaskFactory(blk.meta, blk.scheduler)
	taskType = tasks.DataCompactionTask
	scopes = append(scopes, *blk.meta.AsCommonID())
	return
}

func (blk *dataBlock) IsAppendable() bool {
	if !blk.meta.IsAppendable() {
		return false
	}

	if blk.IsAppendFrozen() {
		return false
	}

	if blk.node.Rows() == blk.meta.GetSegment().GetTable().GetSchema().BlockMaxRows {
		return false
	}
	return true
}

func (blk *dataBlock) GetTotalChanges() int {
	return int(blk.mvcc.GetChangeNodeCnt())
}

func (blk *dataBlock) Rows() int {
	if blk.meta.IsAppendable() {
		rows := int(blk.node.Rows())
		return rows
	}
	metaLoc := blk.meta.GetMetaLoc()
	if metaLoc == "" {
		return 0
	}
	return int(blk.file.ReadRows(metaLoc))
}

// for replay
func (blk *dataBlock) GetRowsOnReplay() uint64 {
	rows := uint64(blk.mvcc.GetTotalRow())
	metaLoc := blk.meta.GetMetaLoc()
	if metaLoc == "" {
		return rows
	}
	fileRows := uint64(blk.file.ReadRows(metaLoc))
	if rows > fileRows {
		return rows
	}
	return fileRows
}

func (blk *dataBlock) PPString(level common.PPLevel, depth int, prefix string) string {
	s := fmt.Sprintf("%s | [Rows=%d]", blk.meta.PPString(level, depth, prefix), blk.Rows())
	if level >= common.PPL1 {
		blk.mvcc.RLock()
		s2 := blk.mvcc.StringLocked()
		blk.mvcc.RUnlock()
		if s2 != "" {
			s = fmt.Sprintf("%s\n%s", s, s2)
		}
	}
	return s
}

func (blk *dataBlock) FillColumnDeletes(view *model.ColumnView, rwlocker *sync.RWMutex) (err error) {
	deleteChain := blk.mvcc.GetDeleteChain()
	n, err := deleteChain.CollectDeletesLocked(view.Ts, false, rwlocker)
	if err != nil {
		return
	}
	dnode := n.(*updates.DeleteNode)
	if dnode != nil {
		view.DeleteMask = dnode.GetDeleteMaskLocked()
	}
	return
}

func (blk *dataBlock) MakeAppender() (appender data.BlockAppender, err error) {
	if !blk.meta.IsAppendable() {
		panic("can not create appender on non-appendable block")
	}
	appender = newAppender(blk.node)
	return
}

func (blk *dataBlock) GetColumnDataByName(
	txn txnif.AsyncTxn,
	attr string,
	buffer *bytes.Buffer) (view *model.ColumnView, err error) {
	colIdx := blk.meta.GetSchema().GetColIdx(attr)
	return blk.GetColumnDataById(txn, colIdx, buffer)
}

func (blk *dataBlock) GetColumnDataById(
	txn txnif.AsyncTxn,
	colIdx int,
	buffer *bytes.Buffer) (view *model.ColumnView, err error) {
	metaLoc := blk.meta.GetVisibleMetaLoc(txn.GetStartTS())
	if metaLoc == "" {
		return blk.ResolveColumnFromANode(txn.GetStartTS(), colIdx, buffer, false)
	}
	view, err = blk.ResolveColumnFromMeta(metaLoc, txn.GetStartTS(), colIdx, buffer)
	return
}

func (blk *dataBlock) ResolveColumnFromMeta(
	metaLoc string,
	ts types.TS,
	idx int,
	buffer *bytes.Buffer) (view *model.ColumnView, err error) {
	view = model.NewColumnView(ts, idx)
	raw, err := blk.LoadColumnData(idx, buffer)
	if err != nil {
		return
	}
	view.SetData(raw)
	err = blk.FillDeltaDeletes(view, ts)
	if err != nil {
		return
	}
	view.SetData(raw)

	blk.mvcc.RLock()
	err = blk.FillColumnDeletes(view, blk.mvcc.RWMutex)
	blk.mvcc.RUnlock()
	if err != nil {
		return
	}
	err = view.Eval(true)
	return
}

func (blk *dataBlock) ResolveColumnFromANode(
	ts types.TS,
	colIdx int,
	buffer *bytes.Buffer,
	raw bool) (view *model.ColumnView, err error) {
	var (
		maxRow  uint32
		visible bool
	)
	blk.mvcc.RLock()
	maxRow, visible, deSels, err := blk.mvcc.GetVisibleRowLocked(ts)
	blk.mvcc.RUnlock()
	if !visible || err != nil {
		return
	}

	view = model.NewColumnView(ts, colIdx)
	var data containers.Vector
	data, err = blk.node.GetColumnData(0, maxRow, colIdx, buffer)
	if err != nil {
		return
	}
	view.SetData(data)
	blk.mvcc.RLock()
	err = blk.FillColumnDeletes(view, blk.mvcc.RWMutex)
	blk.mvcc.RUnlock()
	if err != nil {
		return
	}
	if deSels != nil && !deSels.IsEmpty() {
		if view.DeleteMask != nil {
			view.DeleteMask.Or(deSels)
		} else {
			view.DeleteMask = deSels
		}
	}

	err = view.Eval(true)

	return
}

func (blk *dataBlock) RangeDelete(
	txn txnif.AsyncTxn,
	start, end uint32, dt handle.DeleteType) (node txnif.DeleteNode, err error) {
	blk.mvcc.Lock()
	defer blk.mvcc.Unlock()
	if err = blk.mvcc.CheckNotDeleted(start, end, txn.GetStartTS()); err != nil {
		return
	}
	node = blk.mvcc.CreateDeleteNode(txn, dt)
	node.RangeDeleteLocked(start, end)
	return
}

func (blk *dataBlock) GetValue(txn txnif.AsyncTxn, row, col int) (v any, err error) {
	ts := txn.GetStartTS()
	blk.mvcc.RLock()
	deleted, err := blk.mvcc.IsDeletedLocked(uint32(row), ts, blk.mvcc.RWMutex)
	if err != nil {
		blk.mvcc.RUnlock()
		return
	}
	if deleted {
		err = moerr.NewNotFound()
	}
	blk.mvcc.RUnlock()
	if v != nil || err != nil {
		return
	}
	view := model.NewColumnView(txn.GetStartTS(), int(col))
	metaLoc := blk.meta.GetVisibleMetaLoc(txn.GetStartTS())
	if metaLoc == "" {
		view, _ = blk.ResolveColumnFromANode(txn.GetStartTS(), int(col), nil, true)
	} else {
		vec, _ := blk.LoadColumnData(int(col), nil)
		view.SetData(vec)
	}
	v = view.GetValue(row)
	view.Close()
	return
}

func (blk *dataBlock) LoadColumnData(
	colIdx int,
	buffer *bytes.Buffer) (vec containers.Vector, err error) {
	def := blk.meta.GetSchema().ColDefs[colIdx]
	// FIXME "GetMetaLoc()"
	metaLoc := blk.meta.GetMetaLoc()
	id := blk.meta.AsCommonID()
	id.Idx = uint16(colIdx)
	return evictable.FetchColumnData(buffer, blk.bufMgr, id, blk.colObjects[colIdx], metaLoc, def)
}

func (blk *dataBlock) ResolveDelta(ts types.TS) (bat *containers.Batch, err error) {
	deltaloc := blk.meta.GetDeltaLoc()
	if deltaloc == "" {
		return nil, nil
	}
	bat = containers.NewBatch()
	colNames := []string{catalog.PhyAddrColumnName, catalog.AttrCommitTs, catalog.AttrAborted}
	colTypes := []types.Type{types.T_Rowid.ToType(), types.T_TS.ToType(), types.T_bool.ToType()}
	for i := 0; i < 3; i++ {
		vec, err := evictable.FetchDeltaData(nil, blk.bufMgr, blk.file, deltaloc, uint16(i), colTypes[i])
		if err != nil {
			return bat, err
		}
		bat.AddVector(colNames[i], vec)
	}
	return
}

func (blk *dataBlock) FillDeltaDeletes(
	view *model.ColumnView,
	ts types.TS) (err error) {
	deletes, err := blk.ResolveDelta(ts)
	if deletes == nil {
		return nil
	}
	if err != nil {
		return
	}
	for i := 0; i < deletes.Length(); i++ {
		abort := deletes.Vecs[2].Get(i).(bool)
		if abort {
			continue
		}
		commitTS := deletes.Vecs[1].Get(i).(types.TS)
		if commitTS.Greater(ts) {
			continue
		}
		rowid := deletes.Vecs[0].Get(i).(types.Rowid)
		_, _, row := model.DecodePhyAddrKey(rowid)
		if view.DeleteMask == nil {
			view.DeleteMask = roaring.NewBitmap()
		}
		view.DeleteMask.Add(row)
	}
	return nil
}

func (blk *dataBlock) ablkGetByFilter(ts types.TS, filter *handle.Filter) (offset uint32, err error) {
	blk.mvcc.RLock()
	defer blk.mvcc.RUnlock()
	offset, err = blk.GetActiveRow(filter.Val, ts)
	return
}

func (blk *dataBlock) blkGetByFilter(ts types.TS, filter *handle.Filter) (offset uint32, err error) {
	err = blk.pkIndex.Dedup(filter.Val)
	if err == nil {
		err = moerr.NewNotFound()
		return
	}
	if !moerr.IsMoErrCode(err, moerr.ErrTAEPossibleDuplicate) {
		return
	}
	err = nil
	var sortKey containers.Vector
	if sortKey, err = blk.LoadColumnData(blk.meta.GetSchema().GetSingleSortKeyIdx(), nil); err != nil {
		return
	}
	defer sortKey.Close()
	off, existed := compute.GetOffsetByVal(sortKey, filter.Val, nil)
	if !existed {
		err = moerr.NewNotFound()
		return
	}
	offset = uint32(off)

	blk.mvcc.RLock()
	defer blk.mvcc.RUnlock()
	deleted, err := blk.mvcc.IsDeletedLocked(offset, ts, blk.mvcc.RWMutex)
	if err != nil {
		return
	}
	if deleted {
		err = moerr.NewNotFound()
	}
	return
}

func (blk *dataBlock) GetByFilter(txn txnif.AsyncTxn, filter *handle.Filter) (offset uint32, err error) {
	if filter.Op != handle.FilterEq {
		panic("logic error")
	}
	if blk.meta.GetSchema().SortKey == nil {
		_, _, offset = model.DecodePhyAddrKeyFromValue(filter.Val)
		return
	}
	ts := txn.GetStartTS()
	if blk.meta.IsAppendable() {
		return blk.ablkGetByFilter(ts, filter)
	}
	return blk.blkGetByFilter(ts, filter)
}

func (blk *dataBlock) BlkApplyDelete(deleted uint64, gen common.RowGen, ts types.TS) (err error) {
	blk.meta.GetSegment().GetTable().RemoveRows(deleted)
	return
}

func (blk *dataBlock) OnApplyAppend(n txnif.AppendNode) (err error) {
	blk.meta.GetSegment().GetTable().AddRows(uint64(n.GetMaxRow() - n.GetStartRow()))
	return
}

func (blk *dataBlock) ABlkApplyDelete(deleted uint64, gen common.RowGen, ts types.TS) (err error) {
	blk.meta.GetSegment().GetTable().RemoveRows(deleted)
	return
}

func (blk *dataBlock) GetActiveRow(key any, ts types.TS) (row uint32, err error) {
	rows, err := blk.pkIndex.GetActiveRow(key)
	if err != nil {
		return
	}
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		appendnode := blk.GetAppendNodeByRow(row)
		needWait, txn := appendnode.NeedWaitCommitting(ts)
		if needWait {
			blk.mvcc.RUnlock()
			txn.GetTxnState(true)
			blk.mvcc.RLock()
		}
		if appendnode.IsAborted() || !appendnode.IsVisible(ts) {
			continue
		}
		deleteNode := blk.GetDeleteNodeByRow(row).(*updates.DeleteNode)
		if deleteNode == nil {
			return row, nil
		}
		needWait, txn = deleteNode.NeedWaitCommitting(ts)
		if needWait {
			blk.mvcc.RUnlock()
			txn.GetTxnState(true)
			blk.mvcc.RLock()
		}
		if deleteNode.IsAborted() || !deleteNode.IsVisible(ts) {
			return row, nil
		}
	}
	return 0, moerr.NewNotFound()
}

func (blk *dataBlock) onCheckConflictAndDedup(rowmask *roaring.Bitmap, ts types.TS) func(row uint32) (err error) {
	return func(row uint32) (err error) {
		if rowmask != nil && rowmask.Contains(row) {
			return nil
		}
		appendnode := blk.GetAppendNodeByRow(row)
		needWait, txn := appendnode.NeedWaitCommitting(ts)
		if needWait {
			blk.mvcc.RUnlock()
			txn.GetTxnState(true)
			blk.mvcc.RLock()
		}
		if appendnode.IsAborted() || !appendnode.IsVisible(ts) {
			if err = appendnode.CheckConflict(ts); err != nil {
				return
			}
			return nil
		}
		deleteNode := blk.GetDeleteNodeByRow(row).(*updates.DeleteNode)
		if deleteNode == nil {
			return moerr.NewDuplicate()
		}
		needWait, txn = deleteNode.NeedWaitCommitting(ts)
		if needWait {
			blk.mvcc.RUnlock()
			txn.GetTxnState(true)
			blk.mvcc.RLock()
		}
		if deleteNode.IsAborted() || !deleteNode.IsVisible(ts) {
			return moerr.NewDuplicate()
		}
		if err = appendnode.CheckConflict(ts); err != nil {
			return
		}
		if err = deleteNode.CheckConflict(ts); err != nil {
			return
		}
		return nil
	}
}

func (blk *dataBlock) BatchDedup(txn txnif.AsyncTxn, pks containers.Vector, rowmask *roaring.Bitmap) (err error) {
	if blk.meta.IsAppendable() {
		ts := txn.GetStartTS()
		blk.mvcc.RLock()
		defer blk.mvcc.RUnlock()
		_, err := blk.pkIndex.BatchDedup(pks, blk.onCheckConflictAndDedup(rowmask, ts))
		if err != nil {
			return err
		}

		return err
	}
	if blk.indexes == nil {
		panic("index not found")
	}
	keyselects, err := blk.pkIndex.BatchDedup(pks, blk.onCheckConflictAndDedup(rowmask, txn.GetStartTS()))
	if err == nil {
		return
	}
	if keyselects == nil {
		panic("unexpected error")
	}
	metaLoc := blk.meta.GetMetaLoc()
	sortKey, err := blk.ResolveColumnFromMeta(
		metaLoc,
		txn.GetStartTS(),
		blk.meta.GetSchema().GetSingleSortKeyIdx(),
		nil)
	if err != nil {
		return
	}
	defer sortKey.Close()
	deduplicate := func(v any, _ int) error {
		if _, existed := compute.GetOffsetByVal(sortKey.GetData(), v, sortKey.DeleteMask); existed {
			return moerr.NewDuplicate()
		}
		return nil
	}
	err = pks.Foreach(deduplicate, keyselects)
	return
}

func (blk *dataBlock) CollectAppendLogIndexes(startTs, endTs types.TS) (indexes []*wal.Index, err error) {
	blk.mvcc.RLock()
	defer blk.mvcc.RUnlock()
	return blk.mvcc.CollectAppendLogIndexesLocked(startTs, endTs)
}

func (blk *dataBlock) CollectChangesInRange(startTs, endTs types.TS) (view *model.BlockView, err error) {
	view = model.NewBlockView(endTs)
	blk.mvcc.RLock()
	deleteChain := blk.mvcc.GetDeleteChain()
	view.DeleteMask, view.DeleteLogIndexes, err = deleteChain.CollectDeletesInRange(startTs, endTs, blk.mvcc.RWMutex)
	blk.mvcc.RUnlock()
	return
}

func (blk *dataBlock) CollectAppendInRange(start, end types.TS) (*containers.Batch, error) {
	if blk.meta.IsAppendable() {
		return blk.collectAblkAppendInRange(start, end)
	}
	return nil, nil
}

func (blk *dataBlock) collectAblkAppendInRange(start, end types.TS) (*containers.Batch, error) {
	minRow, maxRow, commitTSVec, abortVec := blk.mvcc.CollectAppend(start, end)
	batch, err := blk.node.GetData(minRow, maxRow)
	if err != nil {
		return nil, err
	}
	batch.AddVector(catalog.AttrCommitTs, commitTSVec)
	batch.AddVector(catalog.AttrAborted, abortVec)
	return batch, nil
}

func (blk *dataBlock) CollectDeleteInRange(start, end types.TS) (*containers.Batch, error) {
	rowID, ts, abort := blk.mvcc.CollectDelete(start, end)
	if rowID == nil {
		return nil, nil
	}
	batch := containers.NewBatch()
	batch.AddVector(catalog.PhyAddrColumnName, rowID)
	batch.AddVector(catalog.AttrCommitTs, ts)
	batch.AddVector(catalog.AttrAborted, abort)
	return batch, nil
}

func (blk *dataBlock) GetAppendNodeByRow(row uint32) txnif.AppendNode {
	return blk.mvcc.GetAppendNodeByRow(row)
}
func (blk *dataBlock) GetDeleteNodeByRow(row uint32) txnif.DeleteNode {
	return blk.mvcc.GetDeleteNodeByRow(row)
}
