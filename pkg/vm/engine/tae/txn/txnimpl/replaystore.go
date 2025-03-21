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

package txnimpl

import (
	"context"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/util/fault"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/catalog"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/logstore/wal"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tables/updates"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/txn/txnbase"
)

var ErrDebugReplay = moerr.NewInternalErrorNoCtx("debug")

type replayTxnStore struct {
	txnbase.NoopTxnStore
	Cmd      *txnbase.TxnCmd
	Observer wal.ReplayObserver
	catalog  *catalog.Catalog
	ctx      context.Context
}

func MakeReplayTxn(
	ctx context.Context,
	mgr *txnbase.TxnManager,
	txnCtx *txnbase.TxnCtx,
	lsn uint64,
	cmd *txnbase.TxnCmd,
	observer wal.ReplayObserver,
	catalog *catalog.Catalog,
) *txnbase.Txn {
	store := &replayTxnStore{
		Cmd:      cmd,
		Observer: observer,
		catalog:  catalog,
		ctx:      ctx,
	}
	txn := txnbase.NewPersistedTxn(
		mgr,
		txnCtx,
		store,
		lsn,
		store.prepareCommit,
		store.prepareRollback,
		store.applyCommit,
		store.applyRollback)
	return txn
}
func (store *replayTxnStore) GetContext() context.Context {
	return store.ctx
}
func (store *replayTxnStore) IsOffline() bool  { return false }
func (store *replayTxnStore) IsReadonly() bool { return false }

func (store *replayTxnStore) prepareCommit(txn txnif.AsyncTxn) (err error) {
	// PrepareCommit all commands
	// Check idempotent of each command
	// Record all idempotent error commands and skip apply|rollback later
	store.Observer.OnTimeStamp(txn.GetPrepareTS())
	for _, command := range store.Cmd.Cmds {
		command.SetReplayTxn(txn)
		store.prepareCmd(command)
	}
	return
}

func (store *replayTxnStore) applyCommit(txn txnif.AsyncTxn) (err error) {
	store.Cmd.ApplyCommit()
	return
}

func (store *replayTxnStore) applyRollback(txn txnif.AsyncTxn) (err error) {
	store.Cmd.ApplyRollback()
	return
}

func (store *replayTxnStore) prepareRollback(txn txnif.AsyncTxn) (err error) {
	panic(moerr.NewInternalErrorNoCtxf("cannot prepareRollback rollback replay txn: %s",
		txn.String()))
}

func (store *replayTxnStore) prepareCmd(txncmd txnif.TxnCmd) {
	if txncmd.GetType() != txnbase.IOET_WALTxnEntry {
		common.DoIfDebugEnabled(func() {
			logutil.Debug(
				"",
				common.OperationField("replay-cmd"),
				common.OperandField(txncmd.Desc()),
			)
		})
	}
	switch cmd := txncmd.(type) {
	case *catalog.EntryCommand[*catalog.EmptyMVCCNode, *catalog.DBNode],
		*catalog.EntryCommand[*catalog.TableMVCCNode, *catalog.TableNode],
		*catalog.EntryCommand[*catalog.MetadataMVCCNode, *catalog.ObjectNode],
		*catalog.EntryCommand[*catalog.ObjectMVCCNode, *catalog.ObjectNode],
		*catalog.EntryCommand[*catalog.MetadataMVCCNode, *catalog.BlockNode]:
		store.catalog.ReplayCmd(txncmd, store.Observer)
	case *AppendCmd:
		store.replayAppendData(
			cmd, store.Observer)
	case *updates.UpdateCmd:
		store.replayDataCmds(
			cmd, store.Observer)
	}
}

func (store *replayTxnStore) replayAppendData(cmd *AppendCmd, observer wal.ReplayObserver) {
	hasActive := false
	_, sarg, _ := fault.TriggerFault("replay debug log")
	for _, info := range cmd.Infos {
		id := info.GetDest()
		database, err := store.catalog.GetDatabaseByID(id.DbID)
		if sarg != "" {
			err = ErrDebugReplay
		}
		if err != nil {
			logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
			if err != ErrDebugReplay {
				panic(err)
			}
		}
		blk, err := database.GetObjectEntryByID(id, cmd.IsTombstone)
		if sarg != "" {
			err = ErrDebugReplay
		}
		if err != nil {
			logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
			if err != ErrDebugReplay {
				panic(err)
			}
		}
		if !blk.IsActive() {
			continue
		}
		if blk.ObjectPersisted() {
			continue
		}
		hasActive = true
	}

	if !hasActive {
		return
	}

	data := cmd.Data
	if data != nil {
		defer data.Close()
	}

	for _, info := range cmd.Infos {
		id := info.GetDest()
		database, err := store.catalog.GetDatabaseByID(id.DbID)
		if sarg != "" {
			err = ErrDebugReplay
		}
		if err != nil {
			logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
			if err != ErrDebugReplay {
				panic(err)
			}
		}
		blk, err := database.GetObjectEntryByID(id, cmd.IsTombstone)
		if sarg != "" {
			err = ErrDebugReplay
		}
		if err != nil {
			logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
			if err != ErrDebugReplay {
				panic(err)
			}
		}
		if !blk.IsActive() {
			continue
		}
		if blk.ObjectPersisted() {
			continue
		}
		start := info.GetSrcOff()
		bat := data.CloneWindow(int(start), int(info.GetSrcLen()))
		bat.Compact()
		defer bat.Close()
		if err = blk.GetObjectData().OnReplayAppendPayload(bat); err != nil || sarg != "" {
			logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
			if sarg == "" {
				panic(err)
			}
		}
	}
}

func (store *replayTxnStore) replayDataCmds(cmd *updates.UpdateCmd, observer wal.ReplayObserver) {
	switch cmd.GetType() {
	case updates.IOET_WALTxnCommand_AppendNode:
		store.replayAppend(cmd, observer)
		// case updates.IOET_WALTxnCommand_DeleteNode, updates.IOET_WALTxnCommand_PersistedDeleteNode:
		// 	store.replayDelete(cmd, observer)
	}
}

func (store *replayTxnStore) replayAppend(cmd *updates.UpdateCmd, observer wal.ReplayObserver) {
	appendNode := cmd.GetAppendNode()
	id := appendNode.GetID()
	database, err := store.catalog.GetDatabaseByID(id.DbID)
	_, sarg, _ := fault.TriggerFault("replay debug log")
	if sarg != "" {
		err = ErrDebugReplay
	}
	if err != nil {
		logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
		if err != ErrDebugReplay {
			panic(err)
		}
	}
	obj, err := database.GetObjectEntryByID(id, cmd.GetAppendNode().IsTombstone())
	if sarg != "" {
		err = ErrDebugReplay
	}
	if err != nil {
		logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
		if err != ErrDebugReplay {
			panic(err)
		}
	}
	if !obj.IsActive() {
		return
	}
	if obj.ObjectPersisted() {
		return
	}
	if err = obj.GetObjectData().OnReplayAppend(appendNode); err != nil || sarg != "" {
		logutil.Infof("cmd %v\ncatalog: %v", cmd.String(), store.catalog.SimplePPString(3))
		if sarg == "" {
			panic(err)
		}
	}
}
