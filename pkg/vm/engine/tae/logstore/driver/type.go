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

package driver

import (
	"context"
	"time"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/logstore/driver/entry"
)

type Driver interface {
	Append(*entry.Entry) error
	Truncate(lsn uint64) error
	GetTruncated() (dsn uint64, err error)
	Close() error
	Replay(
		ctx context.Context,
		h ApplyHandle,
		modeGetter func() ReplayMode,
		opt *ReplayOption,
	) error
	GetDSN() uint64
}

type ReplayEntryState int8

const (
	RE_Truncate ReplayEntryState = iota
	RE_Internal
	RE_Nomal
	RE_Invalid
)

type ApplyHandle = func(*entry.Entry) (replayEntryState ReplayEntryState)

type DriverMode int32

const (
	DriverMode_Invalid DriverMode = iota
	DriverMode_Writable
	DriverMode_Readonly
)

type ReplayOption struct {
	PollTruncateInterval time.Duration // Logservice only
}

type ReplayMode int32

const (
	ReplayMode_Invalid ReplayMode = iota
	ReplayMode_ReplayForWrite
	ReplayMode_ReplayForRead
	ReplayMode_ReplayForever
)
