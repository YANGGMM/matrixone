// Copyright 2020 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

// Portions of this file are additionally subject to the following
// copyright.
//
// Copyright (C) 2021 MatrixOrigin.
//
// Modified the behavior of the operator.

package operator

import (
	"time"

	pb "github.com/matrixorigin/matrixone/pkg/pb/logservice"
)

const (
	ExpireTime         = 15 * time.Second
	NoopEpoch   uint64 = 0
	NoopShardID uint64 = 0
)

// Operator contains execution steps generated by scheduler.
type Operator struct {
	brief string

	shardID uint64
	epoch   uint64

	steps       []OpStep
	currentStep int32

	status OpStatusTracker
}

// NewOperator creates a new operator.
func NewOperator(brief string, shardID uint64, epoch uint64, steps ...OpStep) *Operator {
	return &Operator{
		brief:   brief,
		shardID: shardID,
		epoch:   epoch,

		steps:  steps,
		status: NewOpStatusTracker(),
	}
}

// ShardID returns shard ID.
func (o *Operator) ShardID() uint64 {
	return o.shardID
}

// OpSteps returns operator steps.
func (o *Operator) OpSteps() []OpStep {
	return o.steps
}

// Status returns operator status.
func (o *Operator) Status() OpStatus {
	return o.status.Status()
}

// SetStatus only used for tests.
func (o *Operator) SetStatus(status OpStatus) {
	o.status.setStatus(status)
}

// Cancel marks the operator canceled.
func (o *Operator) Cancel() bool {
	return o.status.To(CANCELED)
}

// HasStarted returns whether operator has started.
func (o *Operator) HasStarted() bool {
	return !o.GetStartTime().IsZero()
}

// GetStartTime gets the start time of operator.
func (o *Operator) GetStartTime() time.Time {
	return o.status.ReachTimeOf(STARTED)
}

// IsEnd checks if the operator is at and end status.
func (o *Operator) IsEnd() bool {
	return o.status.IsEnd()
}

// CheckSuccess checks if all steps are finished, and update the status.
func (o *Operator) CheckSuccess() bool {
	if o.currentStep >= int32(len(o.steps)) {
		return o.status.To(SUCCESS) || o.Status() == SUCCESS
	}
	return false
}

// CheckExpired checks if the operator is timeout, and update the status.
func (o *Operator) CheckExpired() bool {
	if o.CheckSuccess() {
		return false
	}
	return o.status.CheckExpired(ExpireTime)
}

func (o *Operator) Check(logState pb.LogState, dnState pb.DNState) OpStep {
	if o.IsEnd() {
		return nil
	}
	// CheckExpired will call CheckSuccess first
	defer func() { _ = o.CheckExpired() }()
	for step := o.currentStep; int(step) < len(o.steps); step++ {
		if o.steps[int(step)].IsFinish(logState, dnState) {
			o.currentStep = step + 1
		} else {
			return o.steps[int(step)]
		}
	}
	return nil
}
