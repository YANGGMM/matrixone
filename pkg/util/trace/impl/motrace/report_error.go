// Copyright 2022 Matrix Origin
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

package motrace

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cockroachdb/errors/errbase"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/util/errutil"
	"github.com/matrixorigin/matrixone/pkg/util/export/table"
	"github.com/matrixorigin/matrixone/pkg/util/trace"
	"go.uber.org/zap"
)

// MOErrorHolder implement export.IBuffer2SqlItem and export.CsvFields
type MOErrorHolder struct {
	Error     error     `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

var errorPool = sync.Pool{
	New: func() any {
		return &MOErrorHolder{}
	},
}

func newMOErrorHolder(err error, t time.Time) *MOErrorHolder {
	h := errorPool.Get().(*MOErrorHolder)
	h.Error = err
	h.Timestamp = t
	return h
}

func (h *MOErrorHolder) GetName() string {
	return errorView.OriginTable.GetName()
}

func (h *MOErrorHolder) Size() int64 {
	return int64(32*8) + int64(unsafe.Sizeof(h))
}
func (h *MOErrorHolder) Free() {
	h.Error = nil
	errorPool.Put(h)
}

func (h *MOErrorHolder) GetTable() *table.Table { return errorView.OriginTable }

func (h *MOErrorHolder) FillRow(ctx context.Context, row *table.Row) {
	row.Reset()
	row.SetColumnVal(rawItemCol, table.StringField(errorView.Table))
	row.SetColumnVal(timestampCol, table.TimeField(h.Timestamp))
	row.SetColumnVal(nodeUUIDCol, table.StringField(GetNodeResource().NodeUuid))
	row.SetColumnVal(nodeTypeCol, table.StringField(GetNodeResource().NodeType))
	row.SetColumnVal(errorCol, table.StringField(h.Error.Error()))
	row.SetColumnVal(stackCol, table.StringField(fmt.Sprintf(errorFormatter.Load().(string), h.Error)))
	var moError *moerr.Error
	if errors.As(h.Error, &moError) {
		row.SetColumnVal(errCodeCol, table.StringField(fmt.Sprintf("%d", moError.ErrorCode())))
	}
	if ct := errutil.GetContextTracer(h.Error); ct != nil && ct.Context() != nil {
		span := trace.SpanFromContext(ct.Context())
		row.SetColumnVal(traceIDCol, table.StringField(span.SpanContext().TraceID.String()))
		row.SetColumnVal(spanIDCol, table.StringField(span.SpanContext().SpanID.String()))
		row.SetColumnVal(spanKindCol, table.StringField(span.SpanContext().Kind.String()))
	}
}

func (h *MOErrorHolder) Format(s fmt.State, verb rune) { errbase.FormatError(h.Error, s, verb) }

var disableLogErrorReport atomic.Bool

func DisableLogErrorReport(disable bool) {
	disableLogErrorReport.Store(disable)
}

// ReportError send to BatchProcessor
func ReportError(ctx context.Context, err error, depth int) {
	// context ctl
	if errutil.NoReportFromContext(ctx) {
		return
	}
	// global ctl
	if disableLogErrorReport.Load() {
		return
	}
	// log every time
	msg := fmt.Sprintf("error: %v", err)
	sc := trace.SpanFromContext(ctx).SpanContext()
	if sc.IsEmpty() {
		logutil.GetErrorLogger().WithOptions(zap.AddCallerSkip(depth)).Error(msg)
	} else {
		logutil.GetErrorLogger().WithOptions(zap.AddCallerSkip(depth)).Error(msg, trace.ContextField(ctx))
	}
	// record ctrl
	if !GetTracerProvider().IsEnable() {
		return
	}
	if ctx == nil {
		ctx = DefaultContext()
	}
	e := newMOErrorHolder(err, time.Now())
	GetGlobalBatchProcessor().Collect(ctx, e)
}
