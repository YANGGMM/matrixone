// Copyright 2024 Matrix Origin
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

package cdc

import (
	"context"
	"sync"
	"time"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/common/mpool"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/fileservice"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/txn/client"
	v2 "github.com/matrixorigin/matrixone/pkg/util/metric/v2"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
)

var _ Reader = new(tableReader)

type tableReader struct {
	cnTxnClient          client.TxnClient
	cnEngine             engine.Engine
	mp                   *mpool.MPool
	packerPool           *fileservice.Pool[*types.Packer]
	info                 *DbTableInfo
	sinker               Sinker
	wMarkUpdater         IWatermarkUpdater
	tick                 *time.Ticker
	initSnapshotSplitTxn bool
	runningReaders       *sync.Map
	startTs, endTs       types.TS
	noFull               bool

	tableDef                           *plan.TableDef
	insTsColIdx, insCompositedPkColIdx int
	delTsColIdx, delCompositedPkColIdx int
}

var NewTableReader = func(
	cnTxnClient client.TxnClient,
	cnEngine engine.Engine,
	mp *mpool.MPool,
	packerPool *fileservice.Pool[*types.Packer],
	info *DbTableInfo,
	sinker Sinker,
	wMarkUpdater IWatermarkUpdater,
	tableDef *plan.TableDef,
	initSnapshotSplitTxn bool,
	runningReaders *sync.Map,
	startTs, endTs types.TS,
	noFull bool,
) Reader {
	reader := &tableReader{
		cnTxnClient:          cnTxnClient,
		cnEngine:             cnEngine,
		mp:                   mp,
		packerPool:           packerPool,
		info:                 info,
		sinker:               sinker,
		wMarkUpdater:         wMarkUpdater,
		tick:                 time.NewTicker(200 * time.Millisecond),
		initSnapshotSplitTxn: initSnapshotSplitTxn,
		runningReaders:       runningReaders,
		startTs:              startTs,
		endTs:                endTs,
		noFull:               noFull,
		tableDef:             tableDef,
	}

	// batch columns layout:
	// 1. data: user defined cols | cpk (if needed) | commit-ts
	// 2. tombstone: pk/cpk | commit-ts
	reader.insTsColIdx, reader.insCompositedPkColIdx = len(tableDef.Cols)-1, len(tableDef.Cols)-2
	reader.delTsColIdx, reader.delCompositedPkColIdx = 1, 0
	// if single col pk, there's no additional cpk col
	if len(tableDef.Pkey.Names) == 1 {
		reader.insCompositedPkColIdx = int(tableDef.Name2ColIndex[tableDef.Pkey.Names[0]])
	}
	return reader
}

func (reader *tableReader) Close() {
	reader.sinker.Close()
}

func (reader *tableReader) Run(ctx context.Context, ar *ActiveRoutine) {
	key := GenDbTblKey(reader.info.SourceDbName, reader.info.SourceTblName)
	if _, loaded := reader.runningReaders.LoadOrStore(key, reader); loaded {
		logutil.Infof("cdc tableReader(%v).Run: already running, end", reader.info)
		reader.Close()
		return
	}
	logutil.Infof("cdc tableReader(%v).Run: start", reader.info)

	var err error
	defer func() {
		if err != nil {
			if err = reader.wMarkUpdater.SaveErrMsg(reader.info.SourceDbName, reader.info.SourceTblName, err.Error()); err != nil {
				logutil.Infof("cdc tableReader(%v).Run: save err msg failed, err: %v", reader.info, err)
			}
		}
		reader.Close()
		reader.runningReaders.Delete(key)
		logutil.Infof("cdc tableReader(%v).Run: end", reader.info)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ar.Pause:
			return
		case <-ar.Cancel:
			return
		case <-reader.tick.C:
		}

		if err = reader.readTable(ctx, ar); err != nil {
			logutil.Errorf("cdc tableReader(%v) failed, err: %v", reader.info, err)
			return
		}
	}
}

var readTableWithTxn = func(
	reader *tableReader,
	ctx context.Context,
	txnOp client.TxnOperator,
	packer *types.Packer,
	ar *ActiveRoutine,
) (err error) {
	return reader.readTableWithTxn(ctx, txnOp, packer, ar)
}

func (reader *tableReader) readTable(ctx context.Context, ar *ActiveRoutine) (err error) {
	//step1 : create an txnOp
	txnOp, err := GetTxnOp(ctx, reader.cnEngine, reader.cnTxnClient, "readMultipleTables")
	if err != nil {
		return err
	}
	defer func() {
		FinishTxnOp(ctx, err, txnOp, reader.cnEngine)
	}()

	EnterRunSql(txnOp)
	defer func() {
		ExitRunSql(txnOp)
	}()

	if err = GetTxn(ctx, reader.cnEngine, txnOp); err != nil {
		return err
	}

	var packer *types.Packer
	put := reader.packerPool.Get(&packer)
	defer put.Put()

	//step2 : read table
	err = readTableWithTxn(reader, ctx, txnOp, packer, ar)
	// if stale read, try to reset watermark
	if moerr.IsMoErrCode(err, moerr.ErrStaleRead) {
		if !reader.noFull && !reader.startTs.IsEmpty() {
			err = moerr.NewInternalErrorf(ctx, "cdc tableReader(%v) stale read, and startTs(%v) is set, end", reader.info, reader.startTs)
			return
		}

		// reset sinker
		reader.sinker.Reset()
		// reset watermark to startTs, will read from the beginning at next round
		watermark := reader.startTs
		if reader.noFull {
			watermark = types.TimestampToTS(txnOp.SnapshotTS())
		}
		reader.wMarkUpdater.UpdateMem(reader.info.SourceDbName, reader.info.SourceTblName, watermark)
		logutil.Infof("cdc tableReader(%v) reset watermark success", reader.info)
		err = nil
	}
	return
}

func (reader *tableReader) readTableWithTxn(
	ctx context.Context,
	txnOp client.TxnOperator,
	packer *types.Packer,
	ar *ActiveRoutine,
) (err error) {
	var rel engine.Relation
	var changes engine.ChangesHandle

	//step1 : get relation
	if _, _, rel, err = GetRelationById(ctx, reader.cnEngine, txnOp, reader.info.SourceTblId); err != nil {
		return
	}

	//step2 : define time range
	//	from = last wmark
	//  to = txn operator snapshot ts
	fromTs := reader.wMarkUpdater.GetFromMem(reader.info.SourceDbName, reader.info.SourceTblName)
	if !reader.endTs.IsEmpty() && fromTs.GE(&reader.endTs) {
		logutil.Debugf("current watermark(%v) >= endTs(%v), end", fromTs, reader.endTs)
		return
	}
	toTs := types.TimestampToTS(GetSnapshotTS(txnOp))
	if !reader.endTs.IsEmpty() && toTs.GT(&reader.endTs) {
		toTs = reader.endTs
	}

	start := time.Now()
	changes, err = CollectChanges(ctx, rel, fromTs, toTs, reader.mp)
	v2.CdcReadDurationHistogram.Observe(time.Since(start).Seconds())
	if err != nil {
		return
	}
	defer changes.Close()

	//step3: pull data
	var insertData, deleteData *batch.Batch
	var insertAtmBatch, deleteAtmBatch *AtomicBatch
	var hasBegin bool

	defer func() {
		if insertData != nil {
			insertData.Clean(reader.mp)
		}
		if deleteData != nil {
			deleteData.Clean(reader.mp)
		}
		if insertAtmBatch != nil {
			insertAtmBatch.Close()
		}
		if deleteAtmBatch != nil {
			deleteAtmBatch.Close()
		}

		curTs := reader.wMarkUpdater.GetFromMem(reader.info.SourceDbName, reader.info.SourceTblName)
		// if curTs != toTs, means this procedure end abnormally, need to rollback
		// e.g. encounter errors, or interrupted by user (pause/cancel)
		if !curTs.Equal(&toTs) {
			logutil.Errorf("cdc tableReader(%v).readTableWithTxn end abnormally", reader.info)
			if hasBegin {
				reader.sinker.SendRollback()
				reader.sinker.SendDummy()
				if rollbackErr := reader.sinker.Error(); rollbackErr != nil {
					logutil.Errorf("cdc tableReader(%v) send rollback failed, err: %v", reader.info, rollbackErr)
				}
			}
		}
	}()

	allocateAtomicBatchIfNeed := func(atmBatch *AtomicBatch) *AtomicBatch {
		if atmBatch == nil {
			atmBatch = NewAtomicBatch(reader.mp)
		}
		return atmBatch
	}

	var curHint engine.ChangesHandle_Hint
	for {
		select {
		case <-ctx.Done():
			return
		case <-ar.Pause:
			return
		case <-ar.Cancel:
			return
		default:
		}
		// check sinker error of last round
		if err = reader.sinker.Error(); err != nil {
			return
		}

		v2.CdcMpoolInUseBytesGauge.Set(float64(reader.mp.Stats().NumCurrBytes.Load()))
		start = time.Now()
		insertData, deleteData, curHint, err = changes.Next(ctx, reader.mp)
		v2.CdcReadDurationHistogram.Observe(time.Since(start).Seconds())
		if err != nil {
			return
		}

		// both nil denote no more data (end of this tail)
		if insertData == nil && deleteData == nil {
			// heartbeat, send remaining data in sinker
			reader.sinker.Sink(ctx, &DecoderOutput{
				noMoreData: true,
				fromTs:     fromTs,
				toTs:       toTs,
			})

			// send a dummy to guarantee last piece of snapshot/tail send successfully
			reader.sinker.SendDummy()
			if err = reader.sinker.Error(); err == nil {
				if hasBegin {
					// error may not be caught immediately
					reader.sinker.SendCommit()
					// so send a dummy sql to guarantee previous commit is sent successfully
					reader.sinker.SendDummy()
					err = reader.sinker.Error()
				}

				// if commit successfully, update watermark
				if err == nil {
					reader.wMarkUpdater.UpdateMem(reader.info.SourceDbName, reader.info.SourceTblName, toTs)
				}
			}
			return
		}

		addStartMetrics(insertData, deleteData)

		switch curHint {
		case engine.ChangesHandle_Snapshot:
			// output sql in a txn
			if !hasBegin && !reader.initSnapshotSplitTxn {
				reader.sinker.SendBegin()
				hasBegin = true
			}

			// transform into insert instantly
			reader.sinker.Sink(ctx, &DecoderOutput{
				outputTyp:     OutputTypeSnapshot,
				checkpointBat: insertData,
				fromTs:        fromTs,
				toTs:          toTs,
			})
			addSnapshotEndMetrics(insertData)
			insertData.Clean(reader.mp)
		case engine.ChangesHandle_Tail_wip:
			insertAtmBatch = allocateAtomicBatchIfNeed(insertAtmBatch)
			deleteAtmBatch = allocateAtomicBatchIfNeed(deleteAtmBatch)
			insertAtmBatch.Append(packer, insertData, reader.insTsColIdx, reader.insCompositedPkColIdx)
			deleteAtmBatch.Append(packer, deleteData, reader.delTsColIdx, reader.delCompositedPkColIdx)
		case engine.ChangesHandle_Tail_done:
			insertAtmBatch = allocateAtomicBatchIfNeed(insertAtmBatch)
			deleteAtmBatch = allocateAtomicBatchIfNeed(deleteAtmBatch)
			insertAtmBatch.Append(packer, insertData, reader.insTsColIdx, reader.insCompositedPkColIdx)
			deleteAtmBatch.Append(packer, deleteData, reader.delTsColIdx, reader.delCompositedPkColIdx)

			//if !strings.Contains(reader.info.SourceTblName, "order") {
			//	logutil.Errorf("tableReader(%s)[%s, %s], insertAtmBatch: %s, deleteAtmBatch: %s",
			//		reader.info.SourceTblName, fromTs.ToString(), toTs.ToString(),
			//		insertAtmBatch.DebugString(reader.tableDef, false),
			//		deleteAtmBatch.DebugString(reader.tableDef, true))
			//}

			// output sql in a txn
			if !hasBegin {
				reader.sinker.SendBegin()
				hasBegin = true
			}

			reader.sinker.Sink(ctx, &DecoderOutput{
				outputTyp:      OutputTypeTail,
				insertAtmBatch: insertAtmBatch,
				deleteAtmBatch: deleteAtmBatch,
				fromTs:         fromTs,
				toTs:           toTs,
			})
			addTailEndMetrics(insertAtmBatch)
			addTailEndMetrics(deleteAtmBatch)
			insertAtmBatch.Close()
			deleteAtmBatch.Close()
			// reset, allocate new when next wip/done
			insertAtmBatch = nil
			deleteAtmBatch = nil
		}
	}
}
