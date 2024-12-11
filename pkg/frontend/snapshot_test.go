// Copyright 2021 - 2024 Matrix Origin
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

package frontend

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/matrixorigin/matrixone/pkg/config"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/defines"
	"github.com/prashantv/gostub"
	"github.com/smartystreets/goconvey/convey"
)

func Test_getRestoreDropedAccounts(t *testing.T) {
	convey.Convey("getRestoreDropedAccounts ", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ses := newTestSession(t, ctrl)
		defer ses.Close()

		bh := &backgroundExecTest{}
		bh.init()

		bhStub := gostub.StubFunc(&NewBackgroundExec, bh)
		defer bhStub.Reset()

		pu := config.NewParameterUnit(&config.FrontendParameters{}, nil, nil, nil)
		pu.SV.SetDefaultValues()
		setPu("", pu)
		ctx := context.WithValue(context.TODO(), config.ParameterUnitKey, pu)
		rm, _ := NewRoutineManager(ctx, "")
		ses.rm = rm

		tenant := &TenantInfo{
			Tenant:        sysAccountName,
			User:          rootName,
			DefaultRole:   moAdminRoleName,
			TenantID:      sysAccountID,
			UserID:        rootID,
			DefaultRoleID: moAdminRoleID,
		}
		ses.SetTenantInfo(tenant)

		ctx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(sysAccountID))

		_, err := getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql := fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs := newMrsForPitrRecord([][]interface{}{})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldBeNil)

		sql = fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{uint64(0), "sys", "root", "system account"},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldBeNil)

		sql = fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{"abc", "sys", "root", "system account"},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql = fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{uint64(0), types.Day_Hour, "root", "system account"},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql = fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{uint64(0), "sys", types.Day_Hour, "system account"},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql = fmt.Sprintf(getRestoreDropedAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{uint64(0), "sys", "root", types.Day_Hour},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreDropedAccounts(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func Test_getRestoreToDropAccount(t *testing.T) {
	convey.Convey("getRestoreToDropAccount ", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ses := newTestSession(t, ctrl)
		defer ses.Close()

		bh := &backgroundExecTest{}
		bh.init()

		bhStub := gostub.StubFunc(&NewBackgroundExec, bh)
		defer bhStub.Reset()

		pu := config.NewParameterUnit(&config.FrontendParameters{}, nil, nil, nil)
		pu.SV.SetDefaultValues()
		setPu("", pu)
		ctx := context.WithValue(context.TODO(), config.ParameterUnitKey, pu)
		rm, _ := NewRoutineManager(ctx, "")
		ses.rm = rm

		tenant := &TenantInfo{
			Tenant:        sysAccountName,
			User:          rootName,
			DefaultRole:   moAdminRoleName,
			TenantID:      sysAccountID,
			UserID:        rootID,
			DefaultRoleID: moAdminRoleID,
		}
		ses.SetTenantInfo(tenant)

		ctx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(sysAccountID))

		_, err := getRestoreToDropAccount(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql := fmt.Sprintf(getRestoreToDropAccountsFmt, 0)
		mrs := newMrsForPitrRecord([][]interface{}{})
		bh.sql2result[sql] = mrs

		_, err = getRestoreToDropAccount(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldBeNil)

		sql = fmt.Sprintf(getRestoreToDropAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{"sys", uint64(0)},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreToDropAccount(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldBeNil)

		sql = fmt.Sprintf(getRestoreToDropAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{types.Day_Hour, uint64(0)},
			{"sys", uint64(0)},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreToDropAccount(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql = fmt.Sprintf(getRestoreToDropAccountsFmt, 0)
		mrs = newMrsForPitrRecord([][]interface{}{
			{"sys", types.Day_Hour},
		})
		bh.sql2result[sql] = mrs

		_, err = getRestoreToDropAccount(ctx, "", bh, "sp01", 0)
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func Test_restoreAccountUsingClusterSnapshotToNew(t *testing.T) {
	convey.Convey("restoreAccountUsingClusterSnapshotToNew ", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ses := newTestSession(t, ctrl)
		defer ses.Close()

		bh := &backgroundExecTest{}
		bh.init()

		bhStub := gostub.StubFunc(&NewBackgroundExec, bh)
		defer bhStub.Reset()

		pu := config.NewParameterUnit(&config.FrontendParameters{}, nil, nil, nil)
		pu.SV.SetDefaultValues()
		setPu("", pu)
		ctx := context.WithValue(context.TODO(), config.ParameterUnitKey, pu)
		rm, _ := NewRoutineManager(ctx, "")
		ses.rm = rm

		tenant := &TenantInfo{
			Tenant:        sysAccountName,
			User:          rootName,
			DefaultRole:   moAdminRoleName,
			TenantID:      sysAccountID,
			UserID:        rootID,
			DefaultRoleID: moAdminRoleID,
		}
		ses.SetTenantInfo(tenant)

		ctx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(sysAccountID))

		err := restoreAccountUsingClusterSnapshotToNew(ctx, ses, bh, "sp01", 0, accountRecord{accountName: "sys", accountId: 0}, nil, 0)
		convey.So(err, convey.ShouldNotBeNil)

		sql := "select db_name, table_name, refer_db_name, refer_table_name from mo_catalog.mo_foreign_keys"
		mrs := newMrsForPitrRecord([][]interface{}{{"db1", "table1", "db2", "table2"}})
		bh.sql2result[sql] = mrs

		err = restoreAccountUsingClusterSnapshotToNew(ctx, ses, bh, "sp01", 0, accountRecord{accountName: "sys", accountId: 0}, nil, 0)
		convey.So(err, convey.ShouldNotBeNil)
	})
}
