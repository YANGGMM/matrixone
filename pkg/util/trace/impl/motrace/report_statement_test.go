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
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/util/export/table"
	"github.com/matrixorigin/matrixone/pkg/util/trace/impl/motrace/statistic"

	"github.com/google/uuid"
	"github.com/prashantv/gostub"
	"github.com/stretchr/testify/require"

	"github.com/matrixorigin/matrixone/pkg/common/util"
)

func TestStatementInfo_Report_EndStatement(t *testing.T) {
	type fields struct {
		StatementID          [16]byte
		TransactionID        [16]byte
		SessionID            [16]byte
		Account              string
		User                 string
		Host                 string
		Database             string
		Statement            []byte
		StatementFingerprint string
		StatementTag         string
		RequestAt            time.Time
		ExecPlan             SerializableExecPlan
		Status               StatementInfoStatus
		Error                error
		ResponseAt           time.Time
		Duration             time.Duration

		doReport bool
		doExport bool
	}
	type args struct {
		ctx context.Context
		err error
		fun func()
	}

	tests := []struct {
		name          string
		fields        fields
		args          args
		wantReportCnt int
		// check after call EndStatement
		wantReportCntAfterEnd int
	}{
		{
			name: "Report_Export_EndStatement",
			fields: fields{
				doReport: true,
				doExport: true,
			},
			args: args{
				ctx: context.Background(),
				err: nil,
			},
			wantReportCnt:         1,
			wantReportCntAfterEnd: 2,
		},
		{
			name: "Report_EndStatement",
			fields: fields{
				doReport: true,
				doExport: false,
			},
			args: args{
				ctx: context.Background(),
				err: nil,
			},
			wantReportCnt:         1,
			wantReportCntAfterEnd: 1,
		},
		{
			name: "just_EndStatement",
			fields: fields{
				doReport: false,
				doExport: false,
			},
			args: args{
				ctx: context.Background(),
				err: nil,
			},
			wantReportCnt:         0,
			wantReportCntAfterEnd: 1,
		},
		{
			name: "skip_running_stmt",
			fields: fields{
				Status:   StatementStatusRunning,
				doReport: false,
				doExport: false,
			},
			args: args{
				ctx: context.Background(),
				err: nil,
				fun: func() {
					GetTracerProvider().skipRunningStmt = true
				},
			},
			wantReportCnt:         0,
			wantReportCntAfterEnd: 1,
		},
	}

	gotCnt := 0
	dummyReportStmFunc := func(ctx context.Context, s *StatementInfo) error {
		s.reported = true
		gotCnt++
		return nil
	}
	stub := gostub.Stub(&ReportStatement, dummyReportStmFunc)
	defer stub.Reset()

	dummyExport := func(s *StatementInfo) {
		s.mux.Lock()
		s.exported = true
		s.mux.Unlock()
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCnt = 0
			s := &StatementInfo{
				StatementID:          tt.fields.StatementID,
				TransactionID:        tt.fields.TransactionID,
				SessionID:            tt.fields.SessionID,
				Account:              tt.fields.Account,
				User:                 tt.fields.User,
				Host:                 tt.fields.Host,
				Database:             tt.fields.Database,
				Statement:            tt.fields.Statement,
				StatementFingerprint: tt.fields.StatementFingerprint,
				StatementTag:         tt.fields.StatementTag,
				RequestAt:            tt.fields.RequestAt,
				ExecPlan:             tt.fields.ExecPlan,
				Status:               tt.fields.Status,
				Error:                tt.fields.Error,
				ResponseAt:           tt.fields.ResponseAt,
				Duration:             tt.fields.Duration,
			}
			if tt.args.fun != nil {
				tt.args.fun()
			}
			if tt.fields.doExport && !tt.fields.doReport {
				t.Errorf("export(%v) need report(%v) first.", tt.fields.doExport, tt.fields.doReport)
			}
			if tt.fields.doReport {
				s.Report(tt.args.ctx)
			}
			require.Equal(t, tt.wantReportCnt, gotCnt)
			if tt.fields.doExport {
				dummyExport(s)
			}
			require.Equal(t, tt.fields.doReport, s.reported)
			require.Equal(t, tt.fields.doExport, s.exported)

			s.EndStatement(tt.args.ctx, tt.args.err, 0, 0, 0)
			require.Equal(t, tt.wantReportCntAfterEnd, gotCnt)
		})
	}
}

var dummyNoExecPlanJsonResult = `{"code":200,"message":"no exec plan"}`
var dummyNoExecPlanJsonResult2 = `{"func":"dummy2","code":200,"message":"no exec plan"}`

var dummyStatsArray = *statistic.NewStatsArray().WithTimeConsumed(1).WithMemorySize(2).WithS3IOInputCount(3).WithS3IOOutputCount(4).
	WithOutTrafficBytes(5).WithCU(44.0161)

var dummySerializeExecPlan = func(_ context.Context, plan any, _ uuid.UUID) ([]byte, statistic.StatsArray, Statistic) {
	if plan == nil {
		return []byte(dummyNoExecPlanJsonResult), statistic.DefaultStatsArray, Statistic{}
	}
	json, err := json.Marshal(plan)
	if err != nil {
		return []byte(fmt.Sprintf(`{"err": %q}`, err.Error())), statistic.DefaultStatsArray, Statistic{}
	}
	return json, dummyStatsArray, Statistic{RowsRead: 1, BytesScan: 1}
}

var dummySerializeExecPlan2 = func(_ context.Context, plan any, _ uuid.UUID) ([]byte, statistic.StatsArray, Statistic) {
	if plan == nil {
		return []byte(dummyNoExecPlanJsonResult2), statistic.DefaultStatsArray, Statistic{}
	}
	json, err := json.Marshal(plan)
	if err != nil {
		return []byte(fmt.Sprintf(`{"func":"dymmy2","err": %q}`, err.Error())), statistic.DefaultStatsArray, Statistic{}
	}
	val := fmt.Sprintf(`{"func":"dummy2","result":%s}`, json)
	return []byte(val), dummyStatsArray, Statistic{}
}

func TestStatementInfo_ExecPlan2Json(t *testing.T) {
	type args struct {
		ExecPlan          any
		SerializeExecPlan SerializeExecPlanFunc
	}

	dummyExecPlan := map[string]any{"key": "val", "int": 1}
	dummyEPJson := `{"func":"dummy2","result":{"int":1,"key":"val"}}`

	tests := []struct {
		name          string
		args          args
		want          string
		wantStatsByte []byte
	}{
		{
			name: "dummyDefault_ep_Serialize",
			args: args{
				ExecPlan:          dummyExecPlan,
				SerializeExecPlan: dummySerializeExecPlan2,
			},
			want:          dummyEPJson,
			wantStatsByte: dummyStatsArray.ToJsonString(),
		},
		{
			name: "nil_ep_Serialize",
			args: args{
				ExecPlan:          dummyExecPlan,
				SerializeExecPlan: dummySerializeExecPlan2,
			},
			want:          dummyEPJson,
			wantStatsByte: dummyStatsArray.ToJsonString(),
		},
		{
			name: "dummyDefault_nil_Serialize",
			args: args{
				ExecPlan:          nil,
				SerializeExecPlan: dummySerializeExecPlan2,
			},
			want:          dummyNoExecPlanJsonResult2,
			wantStatsByte: statistic.DefaultStatsArrayJsonString,
		},
		{
			name: "nil_nil_Serialize",
			args: args{
				ExecPlan:          nil,
				SerializeExecPlan: dummySerializeExecPlan2,
			},
			want:          dummyNoExecPlanJsonResult2,
			wantStatsByte: statistic.DefaultStatsArrayJsonString,
		},
	}

	ctx := DefaultContext()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StatementInfo{}
			p := &dummySerializableExecPlan{
				plan: tt.args.ExecPlan,
				f:    tt.args.SerializeExecPlan,
			}
			s.SetSerializableExecPlan(p)
			got := s.ExecPlan2Json(ctx)
			err := s.ExecPlan2Stats(ctx)
			require.Nil(t, err)
			stats := s.GetStatsArrayBytes()
			require.Equal(t, tt.want, util.UnsafeBytesToString(got), "ExecPlan2Json()")
			require.Equal(t, tt.wantStatsByte, stats, "want, got: %s, %s", tt.wantStatsByte, stats)
			mapper := new(map[string]any)
			err = json.Unmarshal([]byte(got), mapper)
			require.Nil(t, err, "jons.Unmarshal failed")
		})
	}
}

type dummySerializableExecPlan struct {
	plan any
	f    SerializeExecPlanFunc
	uuid uuid.UUID
}

func NewDummySerializableExecPlan(plan any, f SerializeExecPlanFunc, uuid2 uuid.UUID) *dummySerializableExecPlan {
	return &dummySerializableExecPlan{
		plan: plan,
		f:    f,
		uuid: uuid2,
	}
}

func (p *dummySerializableExecPlan) Marshal(ctx context.Context) []byte {
	res, _, _ := p.f(ctx, p.plan, p.uuid)
	return res
}
func (p *dummySerializableExecPlan) Free() {}

func (p *dummySerializableExecPlan) Stats(ctx context.Context) (statistic.StatsArray, Statistic) {
	_, statByte, stats := p.f(ctx, p.plan, p.uuid)
	return statByte, stats
}

func TestMergeStats(t *testing.T) {
	e := &StatementInfo{}
	e.statsArray.Init().WithTimeConsumed(80335).WithMemorySize(1800).WithS3IOInputCount(1).WithS3IOOutputCount(0).WithConnType(statistic.ConnTypeInternal).WithOutPacketCount(1)

	n := &StatementInfo{}
	n.statsArray.Init().WithTimeConsumed(147960).WithMemorySize(1800).WithS3IOInputCount(0).WithS3IOOutputCount(0).WithOutPacketCount(2)

	err := mergeStats(e, n)
	if err != nil {
		t.Fatalf("mergeStats failed: %v", err)
	}

	wantBytes := []byte("[5,228295,3600.000,1,0,0,1,3,0,0,0]")
	require.Equal(t, wantBytes, e.statsArray.ToJsonString())

	n = &StatementInfo{}
	n.statsArray.Init().WithTimeConsumed(1).WithMemorySize(1).WithS3IOInputCount(0).WithS3IOOutputCount(0).WithOutPacketCount(10).WithCU(1.1234).
		WithS3IOListCount(1).
		WithS3IODeleteCount(1)

	err = mergeStats(e, n)
	if err != nil {
		t.Fatalf("mergeStats failed: %v", err)
	}

	wantBytes = []byte("[5,228296,3601.000,1,0,0,1,13,1.1234,1,1]")
	require.Equal(t, wantBytes, e.statsArray.ToJsonString())

}

func TestCalculateAggrMemoryBytes(t *testing.T) {
	type fields struct {
		dividend float64
		divisor  float64
	}

	tests := []struct {
		name   string
		fields fields
		want   float64
	}{
		{
			name: "normal",
			fields: fields{
				dividend: 1.0,
				divisor:  3.0,
			},
			want: 0.333333,
		},
		{
			name: "1/8",
			fields: fields{
				dividend: 1.0,
				divisor:  8.0,
			},
			want: 0.125,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			val1 := mustDecimal128(convertFloat64ToDecimal128(tt.fields.dividend))
			gotVal := calculateAggrMemoryBytes(val1, tt.fields.divisor)
			require.Equal(t, tt.want, gotVal)
		})
	}
}

func TestStatementInfo_Key(t *testing.T) {

	now := time.Now()
	nowSub5sec := now.Add(-5 * time.Second)
	dummyError := moerr.NewInternalError(context.TODO(), "dummy test errmsg")
	type fields struct {
		SessionID     [16]byte
		SqlSourceType string
		StatementType string
		Status        StatementInfoStatus
		Error         error
		ResponseAt    time.Time
	}
	type args struct {
		duration time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   table.WindowKey
	}{
		{
			name: "normal",
			fields: fields{
				SessionID:     NilSesID,
				SqlSourceType: "source_01",
				StatementType: "stmt_type_01",
				Status:        StatementStatusSuccess,
				Error:         nil,
				ResponseAt:    now,
			},
			args: args{duration: 5 * time.Second},
			want: Key{
				SessionID:     NilSesID,
				StatementType: "stmt_type_01",
				Window:        now.Truncate(5 * time.Second),
				Status:        StatementStatusSuccess,
				SqlSourceType: "source_01",
				Error:         "",
			},
		},
		{
			name: "error",
			fields: fields{
				SessionID:     NilSesID,
				SqlSourceType: "source_01",
				StatementType: "stmt_type_01",
				Status:        StatementStatusFailed,
				Error:         moerr.GetOkExpectedEOF(),
				ResponseAt:    nowSub5sec,
			},
			args: args{duration: 5 * time.Second},
			want: Key{
				SessionID:     NilSesID,
				StatementType: "stmt_type_01",
				Window:        nowSub5sec.Truncate(5 * time.Second),
				Status:        StatementStatusFailed,
				SqlSourceType: "source_01",
				Error:         moerr.GetOkExpectedEOF().Error(),
			},
		},
		{
			name: "error_InternalError",
			fields: fields{
				SessionID:     NilSesID,
				SqlSourceType: "source_01",
				StatementType: "stmt_type_01",
				Status:        StatementStatusFailed,
				Error:         dummyError,
				ResponseAt:    nowSub5sec,
			},
			args: args{duration: 5 * time.Second},
			want: Key{
				SessionID:     NilSesID,
				StatementType: "stmt_type_01",
				Window:        nowSub5sec.Truncate(5 * time.Second),
				Status:        StatementStatusFailed,
				SqlSourceType: "source_01",
				Error:         dummyError.Error(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StatementInfo{
				SessionID:     tt.fields.SessionID,
				SqlSourceType: tt.fields.SqlSourceType,
				StatementType: tt.fields.StatementType,
				Status:        tt.fields.Status,
				Error:         tt.fields.Error,
				ResponseAt:    tt.fields.ResponseAt,
			}
			assert.Equalf(t, tt.want, s.Key(tt.args.duration), "Key(%v)", tt.args.duration)
			t.Logf("error: %v", tt.want.(Key).Error)
		})
	}
}

// Benchmark_Bytes2String
// goos: darwin
// goarch: amd64
// pkg: github.com/matrixorigin/matrixone/pkg/util/trace/impl/motrace
// cpu: Intel(R) Core(TM) i7-8750H CPU @ 2.20GHz
//
//	report_statement_test.go:505: str length: 5155
//
// Benchmark_Bytes2String/byte2string
// Benchmark_Bytes2String/byte2string-12         	  620806	      1721 ns/op
// Benchmark_Bytes2String/byte2stringNoTurn
// Benchmark_Bytes2String/byte2stringNoTurn-12   	 1437241	       829.6 ns/op
// Benchmark_Bytes2String/byte2StringBuilder
// Benchmark_Bytes2String/byte2StringBuilder-12  	 1354514	       852.2 ns/op
// Benchmark_Bytes2String/nil2StringBuilder
// Benchmark_Bytes2String/nil2StringBuilder-12   	350804308	         3.371 ns/op
// PASS
func Benchmark_Bytes2String(b *testing.B) {
	str := []byte("iam a new string sql with 5120 char")
	for i := 0; i < 5120; i++ {
		str = append(str, byte(i))
	}
	b.Logf("str length: %d\n", len(str))
	b.Run("byte2string", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := make([]byte, len(str))
			copy(buf, str)
			_ = string(buf[:])
		}
	})
	b.Run("byte2stringNoTurn", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := make([]byte, 0, len(str))
			_ = append(buf, str...)
		}
	})
	b.Run("byte2StringBuilder", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			builder := strings.Builder{}
			builder.Grow(len(str))
			builder.Write(str)
			_ = builder.String()
		}
	})
	b.Run("nil2StringBuilder", func(b *testing.B) {
		var nilStr []byte = nil
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			builder := strings.Builder{}
			builder.Grow(len(nilStr))
			builder.Write(nilStr)
			_ = builder.String()
		}
	})
}

func Test_StringBuilder(t *testing.T) {

	var builder strings.Builder
	builder.Grow(1024)

	builder.WriteString("hello world!")
	str1 := builder.String()

	builder.Reset()
	builder.WriteString("hello kitty!")
	str2 := builder.String()

	runtime.GC()
	builder.Reset()
	builder.WriteString("hello after gc.")
	str3 := builder.String()

	t.Logf("str1: %s", str1)
	t.Logf("str2: %s", str2)
	t.Logf("str3: %s", str3)
}
