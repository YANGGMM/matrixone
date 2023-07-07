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

package frontend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/matrixorigin/matrixone/pkg/catalog"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/common/mpool"
	"github.com/matrixorigin/matrixone/pkg/config"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/defines"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/dialect"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/dialect/mysql"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/tree"
	plan2 "github.com/matrixorigin/matrixone/pkg/sql/plan"
	"github.com/matrixorigin/matrixone/pkg/util/metric/mometric"
	"github.com/matrixorigin/matrixone/pkg/util/sysview"
	"github.com/matrixorigin/matrixone/pkg/util/trace"
	"github.com/matrixorigin/matrixone/pkg/util/trace/impl/motrace"
	"github.com/tidwall/btree"
)

type TenantInfo struct {
	Tenant      string
	User        string
	DefaultRole string

	TenantID      uint32
	UserID        uint32
	DefaultRoleID uint32

	// true: use secondary role all
	// false: use secondary role none
	useAllSecondaryRole bool

	delimiter byte

	version string
}

func (ti *TenantInfo) String() string {
	delimiter := ti.delimiter
	if !strconv.IsPrint(rune(delimiter)) {
		delimiter = ':'
	}
	return fmt.Sprintf("{account %s%c%s%c%s -- %d%c%d%c%d}",
		ti.Tenant, delimiter, ti.User, delimiter, ti.DefaultRole,
		ti.TenantID, delimiter, ti.UserID, delimiter, ti.DefaultRoleID)
}

func (ti *TenantInfo) GetTenant() string {
	return ti.Tenant
}

func (ti *TenantInfo) GetTenantID() uint32 {
	return ti.TenantID
}

func (ti *TenantInfo) SetTenantID(id uint32) {
	ti.TenantID = id
}

func (ti *TenantInfo) GetUser() string {
	return ti.User
}

func (ti *TenantInfo) SetUser(user string) {
	ti.User = user
}

func (ti *TenantInfo) GetUserID() uint32 {
	return ti.UserID
}

func (ti *TenantInfo) SetUserID(id uint32) {
	ti.UserID = id
}

func (ti *TenantInfo) GetDefaultRole() string {
	return ti.DefaultRole
}

func (ti *TenantInfo) SetDefaultRole(r string) {
	ti.DefaultRole = r
}

func (ti *TenantInfo) HasDefaultRole() bool {
	return len(ti.GetDefaultRole()) != 0
}

func (ti *TenantInfo) GetDefaultRoleID() uint32 {
	return ti.DefaultRoleID
}

func (ti *TenantInfo) SetDefaultRoleID(id uint32) {
	ti.DefaultRoleID = id
}

func (ti *TenantInfo) IsSysTenant() bool {
	return strings.ToLower(ti.GetTenant()) == GetDefaultTenant()
}

func (ti *TenantInfo) IsDefaultRole() bool {
	return ti.GetDefaultRole() == GetDefaultRole()
}

func (ti *TenantInfo) IsMoAdminRole() bool {
	return ti.IsSysTenant() && strings.ToLower(ti.GetDefaultRole()) == moAdminRoleName
}

func (ti *TenantInfo) IsAccountAdminRole() bool {
	return !ti.IsSysTenant() && strings.ToLower(ti.GetDefaultRole()) == accountAdminRoleName
}

func (ti *TenantInfo) IsAdminRole() bool {
	return ti.IsMoAdminRole() || ti.IsAccountAdminRole()
}

func (ti *TenantInfo) IsNameOfAdminRoles(name string) bool {
	n := strings.ToLower(name)
	if ti.IsSysTenant() {
		return n == moAdminRoleName
	} else {
		return n == accountAdminRoleName
	}
}

func (ti *TenantInfo) SetUseSecondaryRole(v bool) {
	ti.useAllSecondaryRole = v
}

func (ti *TenantInfo) GetUseSecondaryRole() bool {
	return ti.useAllSecondaryRole
}

func (ti *TenantInfo) GetVersion() string {
	return ti.version
}

func (ti *TenantInfo) SetVersion(version string) {
	ti.version = version
}

func GetDefaultTenant() string {
	return sysAccountName
}

func GetDefaultRole() string {
	return moAdminRoleName
}

func isCaseInsensitiveEqual(n string, role string) bool {
	return strings.ToLower(n) == role
}

func isPublicRole(n string) bool {
	return isCaseInsensitiveEqual(n, publicRoleName)
}

func isPredefinedRole(r string) bool {
	n := strings.ToLower(r)
	return n == publicRoleName || n == accountAdminRoleName || n == moAdminRoleName
}

func isSysTenant(n string) bool {
	return isCaseInsensitiveEqual(n, sysAccountName)
}

// splitUserInput splits user input into account info
func splitUserInput(ctx context.Context, userInput string, delimiter byte) (*TenantInfo, error) {
	p := strings.IndexByte(userInput, delimiter)
	if p == -1 {
		return &TenantInfo{
			Tenant:    GetDefaultTenant(),
			User:      userInput,
			delimiter: delimiter,
		}, nil
	} else {
		tenant := userInput[:p]
		tenant = strings.TrimSpace(tenant)
		if len(tenant) == 0 {
			return &TenantInfo{}, moerr.NewInternalError(ctx, "invalid tenant name '%s'", tenant)
		}
		userRole := userInput[p+1:]
		p2 := strings.IndexByte(userRole, delimiter)
		if p2 == -1 {
			//tenant:user
			user := userRole
			user = strings.TrimSpace(user)
			if len(user) == 0 {
				return &TenantInfo{}, moerr.NewInternalError(ctx, "invalid user name '%s'", user)
			}
			return &TenantInfo{
				Tenant:    tenant,
				User:      user,
				delimiter: delimiter,
			}, nil
		} else {
			user := userRole[:p2]
			user = strings.TrimSpace(user)
			if len(user) == 0 {
				return &TenantInfo{}, moerr.NewInternalError(ctx, "invalid user name '%s'", user)
			}
			role := userRole[p2+1:]
			role = strings.TrimSpace(role)
			if len(role) == 0 {
				return &TenantInfo{}, moerr.NewInternalError(ctx, "invalid role name '%s'", role)
			}
			return &TenantInfo{
				Tenant:      tenant,
				User:        user,
				DefaultRole: role,
				delimiter:   delimiter,
			}, nil
		}
	}
}

//GetTenantInfo extract tenant info from the input of the user.
/**
The format of the user
1. tenant:user:role
2. tenant:user
3. user

a new format:
1. tenant#user#role
2. tenant#user
*/
func GetTenantInfo(ctx context.Context, userInput string) (*TenantInfo, error) {
	if strings.IndexByte(userInput, ':') != -1 {
		return splitUserInput(ctx, userInput, ':')
	} else if strings.IndexByte(userInput, '#') != -1 {
		return splitUserInput(ctx, userInput, '#')
	} else if strings.Contains(userInput, "%3A") {
		newUserInput := strings.ReplaceAll(userInput, "%3A", ":")
		return splitUserInput(ctx, newUserInput, ':')
	}
	return splitUserInput(ctx, userInput, ':')
}

// initUser for initialization or something special
type initUser struct {
	account  *TenantInfo
	password []byte
}

var (
	specialUsers atomic.Value
)

// SetSpecialUser saves the user for initialization
// !!!NOTE: userName must not contain Colon ':'
func SetSpecialUser(userName string, password []byte) {
	acc := &TenantInfo{
		Tenant:        sysAccountName,
		User:          userName,
		DefaultRole:   moAdminRoleName,
		TenantID:      sysAccountID,
		UserID:        math.MaxUint32,
		DefaultRoleID: moAdminRoleID,
	}

	user := &initUser{
		account:  acc,
		password: password,
	}
	users := getSpecialUsers()
	if users == nil {
		users = make(map[string]*initUser)
	}
	users[userName] = user

	specialUsers.Store(users)
}

// isSpecialUser checks the user is the one for initialization
func isSpecialUser(userName string) (bool, []byte, *TenantInfo) {
	users := getSpecialUsers()

	if len(users) > 0 && users[userName] != nil {
		return true, users[userName].password, users[userName].account
	}
	return false, nil, nil
}

// getSpecialUsers loads the user for initialization
func getSpecialUsers() map[string]*initUser {
	value := specialUsers.Load()
	if value == nil {
		return nil
	}
	return value.(map[string]*initUser)
}

const (
// createMoUserIndex      = 0
// createMoAccountIndex = 1
// createMoRoleIndex      = 2
// createMoUserGrantIndex = 3
// createMoRoleGrantIndex = 4
// createMoRolePrivIndex  = 5
)

const (
	//tenant
	sysAccountID       = 0
	sysAccountName     = "sys"
	sysAccountStatus   = "open"
	sysAccountComments = "system account"

	//role
	moAdminRoleID   = 0
	moAdminRoleName = "moadmin"
	//	moAdminRoleComment      = "super admin role"
	publicRoleID   = 1
	publicRoleName = "public"
	//	publicRoleComment       = "public role"
	accountAdminRoleID   = 2
	accountAdminRoleName = "accountadmin"
	//	accountAdminRoleComment = "account admin role"

	//user
	userStatusLock   = "lock"
	userStatusUnlock = "unlock"

	defaultPasswordEnv = "DEFAULT_PASSWORD"

	rootID            = 0
	rootHost          = "localhost"
	rootName          = "root"
	rootPassword      = "111"
	rootStatus        = userStatusUnlock
	rootExpiredTime   = "NULL"
	rootLoginType     = "PASSWORD"
	rootCreatorID     = rootID
	rootOwnerRoleID   = moAdminRoleID
	rootDefaultRoleID = moAdminRoleID

	dumpID   = 1
	dumpHost = "localhost"
	dumpName = "dump"
	//dumpPassword      = "111"
	dumpStatus        = userStatusUnlock
	dumpExpiredTime   = "NULL"
	dumpLoginType     = "PASSWORD"
	dumpCreatorID     = rootID
	dumpOwnerRoleID   = moAdminRoleID
	dumpDefaultRoleID = moAdminRoleID

	moCatalog = "mo_catalog"
)

type objectType int

const (
	objectTypeDatabase objectType = iota
	objectTypeTable
	objectTypeFunction
	objectTypeAccount
	objectTypeNone

	objectIDAll = 0 //denotes all objects in the object type
)

func (ot objectType) String() string {
	switch ot {
	case objectTypeDatabase:
		return "database"
	case objectTypeTable:
		return "table"
	case objectTypeFunction:
		return "function"
	case objectTypeAccount:
		return "account"
	case objectTypeNone:
		return "none"
	}
	panic("unsupported object type")
}

type privilegeLevelType int

const (
	//*
	privilegeLevelStar privilegeLevelType = iota
	//*.*
	privilegeLevelStarStar
	//db_name
	privilegeLevelDatabase
	//db_name.*
	privilegeLevelDatabaseStar
	//db_name.tbl_name
	privilegeLevelDatabaseTable
	//tbl_name
	privilegeLevelTable
	//db_name.routine_name
	privilegeLevelRoutine
	//
	privilegeLevelEnd
)

func (plt privilegeLevelType) String() string {
	switch plt {
	case privilegeLevelStar:
		return "*"
	case privilegeLevelStarStar:
		return "*.*"
	case privilegeLevelDatabase:
		return "d"
	case privilegeLevelDatabaseStar:
		return "d.*"
	case privilegeLevelDatabaseTable:
		return "d.t"
	case privilegeLevelTable:
		return "t"
	case privilegeLevelRoutine:
		return "r"
	}
	panic(fmt.Sprintf("no such privilege level type %d", plt))
}

type PrivilegeType int

const (
	PrivilegeTypeCreateAccount PrivilegeType = iota
	PrivilegeTypeDropAccount
	PrivilegeTypeAlterAccount
	PrivilegeTypeCreateUser
	PrivilegeTypeDropUser
	PrivilegeTypeAlterUser
	PrivilegeTypeCreateRole
	PrivilegeTypeDropRole
	PrivilegeTypeAlterRole
	PrivilegeTypeCreateDatabase
	PrivilegeTypeDropDatabase
	PrivilegeTypeShowDatabases
	PrivilegeTypeConnect
	PrivilegeTypeManageGrants
	PrivilegeTypeAccountAll
	PrivilegeTypeAccountOwnership
	PrivilegeTypeUserOwnership
	PrivilegeTypeRoleOwnership
	PrivilegeTypeShowTables
	PrivilegeTypeCreateObject //includes: table, view, stream, sequence, function, dblink,etc
	PrivilegeTypeCreateTable
	PrivilegeTypeCreateView
	PrivilegeTypeDropObject
	PrivilegeTypeDropTable
	PrivilegeTypeDropView
	PrivilegeTypeAlterObject
	PrivilegeTypeAlterTable
	PrivilegeTypeAlterView
	PrivilegeTypeDatabaseAll
	PrivilegeTypeDatabaseOwnership
	PrivilegeTypeSelect
	PrivilegeTypeInsert
	PrivilegeTypeUpdate
	PrivilegeTypeTruncate
	PrivilegeTypeDelete
	PrivilegeTypeReference
	PrivilegeTypeIndex //include create/alter/drop index
	PrivilegeTypeTableAll
	PrivilegeTypeTableOwnership
	PrivilegeTypeExecute
	PrivilegeTypeCanGrantRoleToOthersInCreateUser // used in checking the privilege of CreateUser with the default role
	PrivilegeTypeValues
)

type PrivilegeScope uint8

const (
	PrivilegeScopeSys      PrivilegeScope = 1
	PrivilegeScopeAccount  PrivilegeScope = 2
	PrivilegeScopeUser     PrivilegeScope = 4
	PrivilegeScopeRole     PrivilegeScope = 8
	PrivilegeScopeDatabase PrivilegeScope = 16
	PrivilegeScopeTable    PrivilegeScope = 32
	PrivilegeScopeRoutine  PrivilegeScope = 64
)

func (ps PrivilegeScope) String() string {
	sb := strings.Builder{}
	first := true
	for i := 0; i < 8; i++ {
		var s string
		switch ps & (1 << i) {
		case PrivilegeScopeSys:
			s = "sys"
		case PrivilegeScopeAccount:
			s = "account"
		case PrivilegeScopeUser:
			s = "user"
		case PrivilegeScopeRole:
			s = "role"
		case PrivilegeScopeDatabase:
			s = "database"
		case PrivilegeScopeTable:
			s = "table"
		case PrivilegeScopeRoutine:
			s = "routine"
		default:
			s = ""
		}
		if len(s) != 0 {
			if !first {
				sb.WriteString(",")
			} else {
				first = false
			}
			sb.WriteString(s)
		}
	}
	return sb.String()
}

func (pt PrivilegeType) String() string {
	switch pt {
	case PrivilegeTypeCreateAccount:
		return "create account"
	case PrivilegeTypeDropAccount:
		return "drop account"
	case PrivilegeTypeAlterAccount:
		return "alter account"
	case PrivilegeTypeCreateUser:
		return "create user"
	case PrivilegeTypeDropUser:
		return "drop user"
	case PrivilegeTypeAlterUser:
		return "alter user"
	case PrivilegeTypeCreateRole:
		return "create role"
	case PrivilegeTypeDropRole:
		return "drop role"
	case PrivilegeTypeAlterRole:
		return "alter role"
	case PrivilegeTypeCreateDatabase:
		return "create database"
	case PrivilegeTypeDropDatabase:
		return "drop database"
	case PrivilegeTypeShowDatabases:
		return "show databases"
	case PrivilegeTypeConnect:
		return "connect"
	case PrivilegeTypeManageGrants:
		return "manage grants"
	case PrivilegeTypeAccountAll:
		return "account all"
	case PrivilegeTypeAccountOwnership:
		return "account ownership"
	case PrivilegeTypeUserOwnership:
		return "user ownership"
	case PrivilegeTypeRoleOwnership:
		return "role ownership"
	case PrivilegeTypeShowTables:
		return "show tables"
	case PrivilegeTypeCreateObject:
		return "create object"
	case PrivilegeTypeCreateTable:
		return "create table"
	case PrivilegeTypeCreateView:
		return "create view"
	case PrivilegeTypeDropObject:
		return "drop object"
	case PrivilegeTypeDropTable:
		return "drop table"
	case PrivilegeTypeDropView:
		return "drop view"
	case PrivilegeTypeAlterObject:
		return "alter object"
	case PrivilegeTypeAlterTable:
		return "alter table"
	case PrivilegeTypeAlterView:
		return "alter view"
	case PrivilegeTypeDatabaseAll:
		return "database all"
	case PrivilegeTypeDatabaseOwnership:
		return "database ownership"
	case PrivilegeTypeSelect:
		return "select"
	case PrivilegeTypeInsert:
		return "insert"
	case PrivilegeTypeUpdate:
		return "update"
	case PrivilegeTypeTruncate:
		return "truncate"
	case PrivilegeTypeDelete:
		return "delete"
	case PrivilegeTypeReference:
		return "reference"
	case PrivilegeTypeIndex:
		return "index"
	case PrivilegeTypeTableAll:
		return "table all"
	case PrivilegeTypeTableOwnership:
		return "table ownership"
	case PrivilegeTypeExecute:
		return "execute"
	case PrivilegeTypeValues:
		return "values"
	}
	panic(fmt.Sprintf("no such privilege type %d", pt))
}

func (pt PrivilegeType) Scope() PrivilegeScope {
	switch pt {
	case PrivilegeTypeCreateAccount:
		return PrivilegeScopeSys
	case PrivilegeTypeDropAccount:
		return PrivilegeScopeSys
	case PrivilegeTypeAlterAccount:
		return PrivilegeScopeSys
	case PrivilegeTypeCreateUser:
		return PrivilegeScopeAccount
	case PrivilegeTypeDropUser:
		return PrivilegeScopeAccount
	case PrivilegeTypeAlterUser:
		return PrivilegeScopeAccount
	case PrivilegeTypeCreateRole:
		return PrivilegeScopeAccount
	case PrivilegeTypeDropRole:
		return PrivilegeScopeAccount
	case PrivilegeTypeAlterRole:
		return PrivilegeScopeAccount
	case PrivilegeTypeCreateDatabase:
		return PrivilegeScopeAccount
	case PrivilegeTypeDropDatabase:
		return PrivilegeScopeAccount
	case PrivilegeTypeShowDatabases:
		return PrivilegeScopeAccount
	case PrivilegeTypeConnect:
		return PrivilegeScopeAccount
	case PrivilegeTypeManageGrants:
		return PrivilegeScopeAccount
	case PrivilegeTypeAccountAll:
		return PrivilegeScopeAccount
	case PrivilegeTypeAccountOwnership:
		return PrivilegeScopeAccount
	case PrivilegeTypeUserOwnership:
		return PrivilegeScopeUser
	case PrivilegeTypeRoleOwnership:
		return PrivilegeScopeRole
	case PrivilegeTypeShowTables:
		return PrivilegeScopeDatabase
	case PrivilegeTypeCreateObject, PrivilegeTypeCreateTable, PrivilegeTypeCreateView:
		return PrivilegeScopeDatabase
	case PrivilegeTypeDropObject, PrivilegeTypeDropTable, PrivilegeTypeDropView:
		return PrivilegeScopeDatabase
	case PrivilegeTypeAlterObject, PrivilegeTypeAlterTable, PrivilegeTypeAlterView:
		return PrivilegeScopeDatabase
	case PrivilegeTypeDatabaseAll:
		return PrivilegeScopeDatabase
	case PrivilegeTypeDatabaseOwnership:
		return PrivilegeScopeDatabase
	case PrivilegeTypeSelect:
		return PrivilegeScopeTable
	case PrivilegeTypeInsert:
		return PrivilegeScopeTable
	case PrivilegeTypeUpdate:
		return PrivilegeScopeTable
	case PrivilegeTypeTruncate:
		return PrivilegeScopeTable
	case PrivilegeTypeDelete:
		return PrivilegeScopeTable
	case PrivilegeTypeReference:
		return PrivilegeScopeTable
	case PrivilegeTypeIndex:
		return PrivilegeScopeTable
	case PrivilegeTypeTableAll:
		return PrivilegeScopeTable
	case PrivilegeTypeTableOwnership:
		return PrivilegeScopeTable
	case PrivilegeTypeExecute:
		return PrivilegeScopeTable
	case PrivilegeTypeValues:
		return PrivilegeScopeTable
	}
	panic(fmt.Sprintf("no such privilege type %d", pt))
}

var (
	sysWantedDatabases = map[string]int8{
		"mo_catalog":         0,
		"information_schema": 0,
		"system":             0,
		"system_metrics":     0,
	}
	sysDatabases = map[string]int8{
		"mo_catalog":         0,
		"information_schema": 0,
		"system":             0,
		"system_metrics":     0,
		"mysql":              0,
		"mo_task":            0,
	}
	sysWantedTables = map[string]int8{
		"mo_user":                     0,
		"mo_account":                  0,
		"mo_role":                     0,
		"mo_user_grant":               0,
		"mo_role_grant":               0,
		"mo_role_privs":               0,
		"mo_user_defined_function":    0,
		"mo_stored_procedure":         0,
		"mo_mysql_compatibility_mode": 0,
		catalog.MOAutoIncrTable:       0,
	}
	configInitVariables = map[string]int8{
		"save_query_result":      0,
		"query_result_maxsize":   0,
		"query_result_timeout":   0,
		"lower_case_table_names": 0,
	}
	//predefined tables of the database mo_catalog in every account
	predefinedTables = map[string]int8{
		"mo_database":                 0,
		"mo_tables":                   0,
		"mo_columns":                  0,
		"mo_account":                  0,
		"mo_user":                     0,
		"mo_role":                     0,
		"mo_user_grant":               0,
		"mo_role_grant":               0,
		"mo_role_privs":               0,
		"mo_user_defined_function":    0,
		"mo_stored_procedure":         0,
		"mo_mysql_compatibility_mode": 0,
		catalog.MOAutoIncrTable:       0,
		"mo_indexes":                  0,
		"mo_pubs":                     0,
	}
	createDbInformationSchemaSql = "create database information_schema;"
	createAutoTableSql           = fmt.Sprintf(`create table if not exists %s (
		table_id   bigint unsigned, 
		col_name     varchar(770), 
		col_index      int,
		offset     bigint unsigned, 
		step       bigint unsigned,  
		primary key(table_id, col_name)
	);`, catalog.MOAutoIncrTable)
	// mo_indexes is a data dictionary table, must be created first when creating tenants, and last when deleting tenants
	// mo_indexes table does not have `auto_increment` column,
	createMoIndexesSql = `create table mo_indexes(
				id 			bigint unsigned not null,
				table_id 	bigint unsigned not null,
				database_id bigint unsigned not null,
				name 		varchar(64) not null,
				type        varchar(11) not null,
				is_visible  tinyint not null,
				hidden      tinyint not null,
				comment 	varchar(2048) not null,
				column_name    varchar(256) not null,
				ordinal_position  int unsigned  not null,
				options     text,
				index_table_name varchar(5000),
				primary key(id, column_name)
			);`

	//the sqls creating many tables for the tenant.
	//Wrap them in a transaction
	createSqls = []string{
		`create table mo_user(
				user_id int signed auto_increment primary key,
				user_host varchar(100),
				user_name varchar(300),
				authentication_string varchar(100),
				status   varchar(8),
				created_time  timestamp,
				expired_time timestamp,
				login_type  varchar(16),
				creator int signed,
				owner int signed,
				default_role int signed
    		);`,
		`create table mo_account(
				account_id int signed auto_increment primary key,
				account_name varchar(300),
				status varchar(300),
				created_time timestamp,
				comments varchar(256),
				version bigint unsigned auto_increment,
				suspended_time timestamp default NULL
			);`,
		`create table mo_role(
				role_id int signed auto_increment primary key,
				role_name varchar(300),
				creator int signed,
				owner int signed,
				created_time timestamp,
				comments text
			);`,
		`create table mo_user_grant(
				role_id int signed,
				user_id int signed,
				granted_time timestamp,
				with_grant_option bool,
				primary key(role_id, user_id)
			);`,
		`create table mo_role_grant(
				granted_id int signed,
				grantee_id int signed,
				operation_role_id int signed,
				operation_user_id int signed,
				granted_time timestamp,
				with_grant_option bool,
				primary key(granted_id, grantee_id)
			);`,
		`create table mo_role_privs(
				role_id int signed,
				role_name  varchar(100),
				obj_type  varchar(16),
				obj_id bigint unsigned,
				privilege_id int,
				privilege_name varchar(100),
				privilege_level varchar(100),
				operation_user_id int unsigned,
				granted_time timestamp,
				with_grant_option bool,
				primary key(role_id, obj_type, obj_id, privilege_id, privilege_level)
			);`,
		`create table mo_user_defined_function(
				function_id int auto_increment,
				name     varchar(100),
				owner  int unsigned,
				args     text,
				retType  varchar(20),
				body     text,
				language varchar(20),
				db       varchar(100),
				definer  varchar(50),
				modified_time timestamp,
				created_time  timestamp,
				type    varchar(10),
				security_type varchar(10), 
				comment  varchar(5000),
				character_set_client varchar(64),
				collation_connection varchar(64),
				database_collation varchar(64),
				primary key(function_id)
			);`,
		`create table mo_mysql_compatibility_mode(
				configuration_id int auto_increment,
				account_id int,
				account_name varchar(300),
				dat_name     varchar(5000) default NULL,
				variable_name  varchar(300),
				variable_value varchar(5000),
				system_variables bool,
				primary key(configuration_id)
			);`,
		`create table mo_pubs(
    		pub_name varchar(64) primary key,
    		database_name varchar(5000),
    		database_id bigint unsigned,
    		all_table bool,
    		table_list text,
    		account_list text,
    		created_time timestamp,
    		owner int unsigned,
    		creator int unsigned,
    		comment text
    		);`,
		`create table mo_stored_procedure(
				proc_id int auto_increment,
				name     varchar(100),
				creator  int unsigned,
				args     text,
				body     text,
				db       varchar(100),
				definer  varchar(50),
				modified_time timestamp,
				created_time  timestamp,
				type    varchar(10),
				security_type varchar(10), 
				comment  varchar(5000),
				character_set_client varchar(64),
				collation_connection varchar(64),
				database_collation varchar(64),
				primary key(proc_id)
			);`,
	}

	//drop tables for the tenant
	dropSqls = []string{
		`drop table if exists mo_catalog.mo_user;`,
		`drop table if exists mo_catalog.mo_role;`,
		`drop table if exists mo_catalog.mo_user_grant;`,
		`drop table if exists mo_catalog.mo_role_grant;`,
		`drop table if exists mo_catalog.mo_role_privs;`,
		`drop table if exists mo_catalog.mo_user_defined_function;`,
		`drop table if exists mo_catalog.mo_stored_procedure;`,
		`drop table if exists mo_catalog.mo_mysql_compatibility_mode;`,
	}
	dropMoPubsSql     = `drop table if exists mo_catalog.mo_pubs;`
	deleteMoPubsSql   = `delete from mo_catalog.mo_pubs;`
	dropAutoIcrColSql = fmt.Sprintf("drop table if exists mo_catalog.`%s`;", catalog.MOAutoIncrTable)
	dropMoIndexes     = `drop table if exists mo_catalog.mo_indexes;`

	initMoMysqlCompatbilityModeFormat = `insert into mo_catalog.mo_mysql_compatibility_mode(
		account_id,
		account_name,
		dat_name,
		variable_name,
		variable_value, system_variables) values (%d, "%s", "%s", "%s", "%s", %v);`

	initMoMysqlCompatbilityModeWithoutDataBaseFormat = `insert into mo_catalog.mo_mysql_compatibility_mode(
		account_id,
		account_name,
		variable_name,
		variable_value, system_variables) values (%d, "%s", "%s", "%s", %v);`

	initMoUserDefinedFunctionFormat = `insert into mo_catalog.mo_user_defined_function(
			name,
			owner,
			args,
			retType,
			body,
			language,
			db,
			definer,
			modified_time,
			created_time,
			type,
			security_type,
			comment,
			character_set_client,
			collation_connection,
			database_collation) values ("%s",%d,'%s',"%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s");`

	initMoStoredProcedureFormat = `insert into mo_catalog.mo_stored_procedure(
		name,
		args,
		body,
		db,
		definer,
		modified_time,
		created_time,
		type,
		security_type,
		comment,
		character_set_client,
		collation_connection,
		database_collation) values ("%s",'%s',"%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s");`

	initMoAccountFormat = `insert into mo_catalog.mo_account(
				account_id,
				account_name,
				status,
				created_time,
				comments) values (%d,"%s","%s","%s","%s");`
	initMoAccountWithoutIDFormat = `insert into mo_catalog.mo_account(
				account_name,
				status,
				created_time,
				comments) values ("%s","%s","%s","%s");`
	initMoRoleFormat = `insert into mo_catalog.mo_role(
				role_id,
				role_name,
				creator,
				owner,
				created_time,
				comments
			) values (%d,"%s",%d,%d,"%s","%s");`
	initMoRoleWithoutIDFormat = `insert into mo_catalog.mo_role(
				role_name,
				creator,
				owner,
				created_time,
				comments
			) values ("%s",%d,%d,"%s","%s");`
	initMoUserFormat = `insert into mo_catalog.mo_user(
				user_id,
				user_host,
				user_name,
				authentication_string,
				status,
				created_time,
				expired_time,
				login_type,
				creator,
				owner,
				default_role
    		) values(%d,"%s","%s","%s","%s","%s",%s,"%s",%d,%d,%d);`
	initMoUserWithoutIDFormat = `insert into mo_catalog.mo_user(
				user_host,
				user_name,
				authentication_string,
				status,
				created_time,
				expired_time,
				login_type,
				creator,
				owner,
				default_role
    		) values("%s","%s","%s","%s","%s",%s,"%s",%d,%d,%d);`
	initMoRolePrivFormat = `insert into mo_catalog.mo_role_privs(
				role_id,
				role_name,
				obj_type,
				obj_id,
				privilege_id,
				privilege_name,
				privilege_level,
				operation_user_id,
				granted_time,
				with_grant_option
			) values(%d,"%s","%s",%d,%d,"%s","%s",%d,"%s",%v);`
	initMoUserGrantFormat = `insert into mo_catalog.mo_user_grant(
            	role_id,
				user_id,
				granted_time,
				with_grant_option
			) values(%d,%d,"%s",%v);`
)

const (
	//privilege verification
	checkTenantFormat = `select account_id,account_name,status,version,suspended_time from mo_catalog.mo_account where account_name = "%s";`

	getTenantNameForMat = `select account_name from mo_catalog.mo_account where account_id = %d;`

	updateCommentsOfAccountFormat = `update mo_catalog.mo_account set comments = "%s" where account_name = "%s";`

	updateStatusOfAccountFormat = `update mo_catalog.mo_account set status = "%s",suspended_time = "%s" where account_name = "%s";`

	updateStatusAndVersionOfAccountFormat = `update mo_catalog.mo_account set status = "%s",version = %d,suspended_time = "%s" where account_name = "%s";`

	deleteAccountFromMoAccountFormat = `delete from mo_catalog.mo_account where account_name = "%s";`

	getPasswordOfUserFormat = `select user_id,authentication_string,default_role from mo_catalog.mo_user where user_name = "%s";`

	updatePasswordOfUserFormat = `update mo_catalog.mo_user set authentication_string = "%s" where user_name = "%s";`

	checkRoleExistsFormat = `select role_id from mo_catalog.mo_role where role_id = %d and role_name = "%s";`

	roleNameOfRoleIdFormat = `select role_name from mo_catalog.mo_role where role_id = %d;`

	roleIdOfRoleFormat = `select role_id from mo_catalog.mo_role where role_name = "%s";`

	//operations on the mo_user_grant
	getRoleOfUserFormat = `select r.role_id from  mo_catalog.mo_role r, mo_catalog.mo_user_grant ug where ug.role_id = r.role_id and ug.user_id = %d and r.role_name = "%s";`

	getRoleIdOfUserIdFormat = `select role_id,with_grant_option from mo_catalog.mo_user_grant where user_id = %d;`

	checkUserGrantFormat = `select role_id,user_id,with_grant_option from mo_catalog.mo_user_grant where role_id = %d and user_id = %d;`

	checkUserHasRoleFormat = `select u.user_id,ug.role_id from mo_catalog.mo_user u, mo_catalog.mo_user_grant ug where u.user_id = ug.user_id and u.user_name = "%s" and ug.role_id = %d;`

	//with_grant_option = true
	checkUserGrantWGOFormat = `select role_id,user_id from mo_catalog.mo_user_grant where with_grant_option = true and role_id = %d and user_id = %d;`

	updateUserGrantFormat = `update mo_catalog.mo_user_grant set granted_time = "%s", with_grant_option = %v where role_id = %d and user_id = %d;`

	insertUserGrantFormat = `insert into mo_catalog.mo_user_grant(role_id,user_id,granted_time,with_grant_option) values (%d,%d,"%s",%v);`

	deleteUserGrantFormat = `delete from mo_catalog.mo_user_grant where role_id = %d and user_id = %d;`

	//operations on the mo_role_grant
	checkRoleGrantFormat = `select granted_id,grantee_id,with_grant_option from mo_catalog.mo_role_grant where granted_id = %d and grantee_id = %d;`

	//with_grant_option = true
	getRoleGrantWGOFormat = `select grantee_id from mo_catalog.mo_role_grant where with_grant_option = true and granted_id = %d;`

	updateRoleGrantFormat = `update mo_catalog.mo_role_grant set operation_role_id = %d, operation_user_id = %d, granted_time = "%s", with_grant_option = %v where granted_id = %d and grantee_id = %d;`

	insertRoleGrantFormat = `insert mo_catalog.mo_role_grant(granted_id,grantee_id,operation_role_id,operation_user_id,granted_time,with_grant_option) values (%d,%d,%d,%d,"%s",%v);`

	deleteRoleGrantFormat = `delete from mo_catalog.mo_role_grant where granted_id = %d and grantee_id = %d;`

	getAllStuffRoleGrantFormat = `select granted_id,grantee_id,with_grant_option from mo_catalog.mo_role_grant;`

	getInheritedRoleIdOfRoleIdFormat = `select granted_id,with_grant_option from mo_catalog.mo_role_grant where grantee_id = %d;`

	checkRoleHasPrivilegeFormat = `select role_id,with_grant_option from mo_catalog.mo_role_privs where role_id = %d and obj_type = "%s" and obj_id = %d and privilege_id = %d;`

	//with_grant_option = true
	checkRoleHasPrivilegeWGOFormat = `select role_id from mo_catalog.mo_role_privs where with_grant_option = true and privilege_id = %d;`

	checkRoleHasPrivilegeWGOOrWithOwnershipFormat = `select distinct role_id from mo_catalog.mo_role_privs where (with_grant_option = true and (privilege_id = %d or privilege_id = %d)) or privilege_id = %d;`

	updateRolePrivsFormat = `update mo_catalog.mo_role_privs set operation_user_id = %d, granted_time = "%s", with_grant_option = %v where role_id = %d and obj_type = "%s" and obj_id = %d and privilege_id = %d;`

	insertRolePrivsFormat = `insert into mo_catalog.mo_role_privs(role_id,role_name,obj_type,obj_id,privilege_id,privilege_name,privilege_level,operation_user_id,granted_time,with_grant_option) 
								values (%d,"%s","%s",%d,%d,"%s","%s",%d,"%s",%v);`

	deleteRolePrivsFormat = `delete from mo_catalog.mo_role_privs 
       									where role_id = %d 
       									    and obj_type = "%s" 
       									    and obj_id = %d 
       									    and privilege_id = %d 
       									    and privilege_level = "%s";`

	checkDatabaseFormat = `select dat_id from mo_catalog.mo_database where datname = "%s";`

	checkDatabaseTableFormat = `select t.rel_id from mo_catalog.mo_database d, mo_catalog.mo_tables t
										where d.dat_id = t.reldatabase_id
											and d.datname = "%s"
											and t.relname = "%s";`

	//TODO:fix privilege_level string and obj_type string
	//For object_type : table, privilege_level : *.*
	checkWithGrantOptionForTableStarStar = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and rp.with_grant_option = true;`

	//For object_type : table, privilege_level : db.*
	checkWithGrantOptionForTableDatabaseStar = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and d.datname = "%s"
					and rp.with_grant_option = true;`

	//For object_type : table, privilege_level : db.table
	checkWithGrantOptionForTableDatabaseTable = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = t.rel_id
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and d.datname = "%s"
					and t.relname = "%s"
					and rp.with_grant_option = true;`

	//For object_type : database, privilege_level : *
	checkWithGrantOptionForDatabaseStar = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and rp.with_grant_option = true;`

	//For object_type : database, privilege_level : *.*
	checkWithGrantOptionForDatabaseStarStar = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and rp.with_grant_option = true;`

	//For object_type : database, privilege_level : db
	checkWithGrantOptionForDatabaseDB = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = d.dat_id
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and  d.datname = "%s"
					and rp.with_grant_option = true;`

	//For object_type : account, privilege_level : *
	checkWithGrantOptionForAccountStar = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and rp.with_grant_option = true;`

	//for database.table or table
	//check the role has the table level privilege for the privilege level (d.t or t)
	checkRoleHasTableLevelPrivilegeFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_tables t, mo_catalog.mo_role_privs rp
				where d.dat_id = t.reldatabase_id
					and rp.obj_id = t.rel_id
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level in ("%s","%s")
					and d.datname = "%s"
					and t.relname = "%s";`

	//for database.* or *
	checkRoleHasTableLevelForDatabaseStarFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_role_privs rp
				where d.dat_id = rp.obj_id
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level in ("%s","%s")
					and d.datname = "%s";`

	//for *.*
	checkRoleHasTableLevelForStarStarFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_role_privs rp
				where rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s";`

	//for * or *.*
	checkRoleHasDatabaseLevelForStarStarFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_role_privs rp
				where rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s";`

	//for database
	checkRoleHasDatabaseLevelForDatabaseFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_database d, mo_catalog.mo_role_privs rp
				where d.dat_id = rp.obj_id
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s"
					and d.datname = "%s";`

	//for *
	checkRoleHasAccountLevelForStarFormat = `select rp.privilege_id,rp.with_grant_option
				from mo_catalog.mo_role_privs rp
				where rp.obj_id = 0
					and rp.obj_type = "%s"
					and rp.role_id = %d
					and rp.privilege_id = %d
					and rp.privilege_level = "%s";`

	getUserRolesExpectPublicRoleFormat = `select role.role_id, role.role_name 
				from mo_catalog.mo_role role, mo_catalog.mo_user_grant mg 
				where role.role_id = mg.role_id 
					and role.role_id != %d  
					and mg.user_id = %d 
					order by role.created_time asc limit 1;`

	checkUdfArgs = `select args,function_id from mo_catalog.mo_user_defined_function where name = "%s" and db = "%s";`

	checkUdfExistence = `select function_id from mo_catalog.mo_user_defined_function where name = "%s" and db = "%s" and args = '%s';`

	checkStoredProcedureArgs = `select proc_id, args from mo_catalog.mo_stored_procedure where name = "%s" and db = "%s";`

	checkProcedureExistence = `select proc_id from mo_catalog.mo_stored_procedure where name = "%s" and db = "%s";`

	//delete role from mo_role,mo_user_grant,mo_role_grant,mo_role_privs
	deleteRoleFromMoRoleFormat = `delete from mo_catalog.mo_role where role_id = %d;`

	deleteRoleFromMoUserGrantFormat = `delete from mo_catalog.mo_user_grant where role_id = %d;`

	deleteRoleFromMoRoleGrantFormat = `delete from mo_catalog.mo_role_grant where granted_id = %d or grantee_id = %d;`

	deleteRoleFromMoRolePrivsFormat = `delete from mo_catalog.mo_role_privs where role_id = %d;`

	// grant ownership on database
	grantOwnershipOnDatabaseFormat = `grant ownership on database %s to %s;`

	// grant ownership on table
	grantOwnershipOnTableFormat = `grant ownership on table %s.%s to %s;`

	// get the owner of the database
	getOwnerOfDatabaseFormat = `select owner from mo_catalog.mo_database where datname = '%s';`

	// get the owner of the table
	getOwnerOfTableFormat = `select owner from mo_catalog.mo_tables where reldatabase = '%s' and relname = '%s';`

	// get the roles of the current user
	getRolesOfCurrentUserFormat = `select role_id from mo_catalog.mo_user_grant where user_id = %d;`

	//delete user from mo_user,mo_user_grant
	deleteUserFromMoUserFormat = `delete from mo_catalog.mo_user where user_id = %d;`

	deleteUserFromMoUserGrantFormat = `delete from mo_catalog.mo_user_grant where user_id = %d;`

	// delete user defined function from mo_user_defined_function
	deleteUserDefinedFunctionFormat = `delete from mo_catalog.mo_user_defined_function where function_id = %d;`

	// delete stored procedure from mo_user_defined_function
	deleteStoredProcedureFormat = `delete from mo_catalog.mo_stored_procedure where proc_id = %d;`

	// delete a tuple from mo_mysql_compatibility_mode when drop a database
	deleteMysqlCompatbilityModeFormat = `delete from mo_catalog.mo_mysql_compatibility_mode where dat_name = "%s";`

	getSystemVariableValueWithDatabaseFormat = `select variable_value from mo_catalog.mo_mysql_compatibility_mode where dat_name = "%s" and variable_name = "%s";`

	getSystemVariablesWithAccountFromat = `select variable_name, variable_value from mo_catalog.mo_mysql_compatibility_mode where account_id = %d and system_variables = true;`

	getSystemVariableValueWithAccountFromat = `select variable_value from mo_catalog.mo_mysql_compatibility_mode where account_id = %d and variable_name = '%s' and system_variables = true;`

	updateSystemVariableValueFormat = `update mo_catalog.mo_mysql_compatibility_mode set variable_value = '%s' where account_id = %d and variable_name = '%s';`

	updateConfigurationByDbNameAndAccountNameFormat = `update mo_catalog.mo_mysql_compatibility_mode set variable_value = '%s' where account_name = '%s' and dat_name = '%s' and variable_name = '%s';`

	updateConfigurationByAccountNameFormat = `update mo_catalog.mo_mysql_compatibility_mode set variable_value = '%s' where account_name = '%s' and variable_name = '%s';`

	getDbIdAndTypFormat         = `select dat_id,dat_type from mo_catalog.mo_database where datname = '%s' and account_id = %d;`
	insertIntoMoPubsFormat      = `insert into mo_catalog.mo_pubs(pub_name,database_name,database_id,all_table,table_list,account_list,created_time,owner,creator,comment) values ('%s','%s',%d,%t,'%s','%s',now(),%d,%d,'%s');`
	getPubInfoFormat            = `select account_list,comment from mo_catalog.mo_pubs where pub_name = '%s';`
	updatePubInfoFormat         = `update mo_catalog.mo_pubs set account_list = '%s',comment = '%s' where pub_name = '%s';`
	dropPubFormat               = `delete from mo_catalog.mo_pubs where pub_name = '%s';`
	getAccountIdAndStatusFormat = `select account_id,status from mo_catalog.mo_account where account_name = '%s';`
	getPubInfoForSubFormat      = `select database_name,account_list from mo_catalog.mo_pubs where pub_name = "%s";`
	getDbPubCountFormat         = `select count(1) from mo_catalog.mo_pubs where database_name = '%s';`

	fetchSqlOfSpFormat = `select body, args from mo_catalog.mo_stored_procedure where name = '%s' and db = '%s';`
)

var (
	objectType2privilegeLevels = map[objectType][]privilegeLevelType{
		objectTypeAccount: {privilegeLevelStar},
		objectTypeDatabase: {privilegeLevelDatabase,
			privilegeLevelStar, privilegeLevelStarStar},
		objectTypeTable: {privilegeLevelStarStar,
			privilegeLevelDatabaseStar, privilegeLevelStar,
			privilegeLevelDatabaseTable, privilegeLevelTable},
	}

	// the databases that can not operated by the real user
	bannedCatalogDatabases = map[string]int8{
		"mo_catalog":         0,
		"information_schema": 0,
		"system":             0,
		"system_metrics":     0,
		"mysql":              0,
		"mo_task":            0,
	}

	// the privileges that can not be granted or revoked
	bannedPrivileges = map[PrivilegeType]int8{
		PrivilegeTypeCreateAccount: 0,
		PrivilegeTypeAlterAccount:  0,
		PrivilegeTypeDropAccount:   0,
	}
)

func getSqlForAccountIdAndStatus(ctx context.Context, accName string, check bool) (string, error) {
	if check && accountNameIsInvalid(accName) {
		return "", moerr.NewInternalError(ctx, fmt.Sprintf("account name %s is invalid", accName))
	}
	return fmt.Sprintf(getAccountIdAndStatusFormat, accName), nil
}

func getSqlForPubInfoForSub(ctx context.Context, pubName string, check bool) (string, error) {
	if check && nameIsInvalid(pubName) {
		return "", moerr.NewInternalError(ctx, fmt.Sprintf("pub name %s is invalid", pubName))
	}
	return fmt.Sprintf(getPubInfoForSubFormat, pubName), nil
}

func getSqlForCheckTenant(ctx context.Context, tenant string) (string, error) {
	err := inputNameIsInvalid(ctx, tenant)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkTenantFormat, tenant), nil
}

func getSqlForGetAccountName(tenantId uint32) string {
	return fmt.Sprintf(getTenantNameForMat, tenantId)
}

func getSqlForUpdateCommentsOfAccount(ctx context.Context, comment, account string) (string, error) {
	err := inputNameIsInvalid(ctx, account)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updateCommentsOfAccountFormat, comment, account), nil
}

func getSqlForUpdateStatusOfAccount(ctx context.Context, status, timestamp, account string) (string, error) {
	err := inputNameIsInvalid(ctx, status, account)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updateStatusOfAccountFormat, status, timestamp, account), nil
}

func getSqlForUpdateStatusAndVersionOfAccount(ctx context.Context, status, timestamp, account string, version uint64) (string, error) {
	err := inputNameIsInvalid(ctx, status, account)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updateStatusAndVersionOfAccountFormat, status, version, timestamp, account), nil
}

func getSqlForDeleteAccountFromMoAccount(ctx context.Context, account string) (string, error) {
	err := inputNameIsInvalid(ctx, account)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(deleteAccountFromMoAccountFormat, account), nil
}

func getSqlForPasswordOfUser(ctx context.Context, user string) (string, error) {
	err := inputNameIsInvalid(ctx, user)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(getPasswordOfUserFormat, user), nil
}

func getSqlForUpdatePasswordOfUser(ctx context.Context, password, user string) (string, error) {
	err := inputNameIsInvalid(ctx, user)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updatePasswordOfUserFormat, password, user), nil
}

func getSqlForCheckRoleExists(ctx context.Context, roleID int, roleName string) (string, error) {
	err := inputNameIsInvalid(ctx, roleName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkRoleExistsFormat, roleID, roleName), nil
}

func getSqlForRoleNameOfRoleId(roleId int64) string {
	return fmt.Sprintf(roleNameOfRoleIdFormat, roleId)
}

func getSqlForRoleIdOfRole(ctx context.Context, roleName string) (string, error) {
	err := inputNameIsInvalid(ctx, roleName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(roleIdOfRoleFormat, roleName), nil
}

func getSqlForRoleOfUser(ctx context.Context, userID int64, roleName string) (string, error) {
	err := inputNameIsInvalid(ctx, roleName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(getRoleOfUserFormat, userID, roleName), nil
}

func getSqlForRoleIdOfUserId(userId int) string {
	return fmt.Sprintf(getRoleIdOfUserIdFormat, userId)
}

func getSqlForCheckUserGrant(roleId, userId int64) string {
	return fmt.Sprintf(checkUserGrantFormat, roleId, userId)
}

func getSqlForCheckUserHasRole(ctx context.Context, userName string, roleId int64) (string, error) {
	err := inputNameIsInvalid(ctx, userName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkUserHasRoleFormat, userName, roleId), nil
}

func getSqlForCheckUserGrantWGO(roleId, userId int64) string {
	return fmt.Sprintf(checkUserGrantWGOFormat, roleId, userId)
}

func getSqlForUpdateUserGrant(roleId, userId int64, timestamp string, withGrantOption bool) string {
	return fmt.Sprintf(updateUserGrantFormat, timestamp, withGrantOption, roleId, userId)
}

func getSqlForInsertUserGrant(roleId, userId int64, timestamp string, withGrantOption bool) string {
	return fmt.Sprintf(insertUserGrantFormat, roleId, userId, timestamp, withGrantOption)
}

func getSqlForDeleteUserGrant(roleId, userId int64) string {
	return fmt.Sprintf(deleteUserGrantFormat, roleId, userId)
}

func getSqlForCheckRoleGrant(grantedId, granteeId int64) string {
	return fmt.Sprintf(checkRoleGrantFormat, grantedId, granteeId)
}

func getSqlForCheckRoleGrantWGO(grantedId int64) string {
	return fmt.Sprintf(getRoleGrantWGOFormat, grantedId)
}

func getSqlForUpdateRoleGrant(grantedId, granteeId, operationRoleId, operationUserId int64, timestamp string, withGrantOption bool) string {
	return fmt.Sprintf(updateRoleGrantFormat, operationRoleId, operationUserId, timestamp, withGrantOption, grantedId, granteeId)
}

func getSqlForInsertRoleGrant(grantedId, granteeId, operationRoleId, operationUserId int64, timestamp string, withGrantOption bool) string {
	return fmt.Sprintf(insertRoleGrantFormat, grantedId, granteeId, operationRoleId, operationUserId, timestamp, withGrantOption)
}

func getSqlForDeleteRoleGrant(grantedId, granteeId int64) string {
	return fmt.Sprintf(deleteRoleGrantFormat, grantedId, granteeId)
}

func getSqlForGetAllStuffRoleGrantFormat() string {
	return getAllStuffRoleGrantFormat
}

func getSqlForInheritedRoleIdOfRoleId(roleId int64) string {
	return fmt.Sprintf(getInheritedRoleIdOfRoleIdFormat, roleId)
}

func getSqlForCheckRoleHasPrivilege(roleId int64, objType objectType, objId, privilegeId int64) string {
	return fmt.Sprintf(checkRoleHasPrivilegeFormat, roleId, objType, objId, privilegeId)
}

func getSqlForCheckRoleHasPrivilegeWGO(privilegeId int64) string {
	return fmt.Sprintf(checkRoleHasPrivilegeWGOFormat, privilegeId)
}

func getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(privilegeId, allPrivId, ownershipPrivId int64) string {
	return fmt.Sprintf(checkRoleHasPrivilegeWGOOrWithOwnershipFormat, privilegeId, allPrivId, ownershipPrivId)
}

func getSqlForUpdateRolePrivs(userId int64, timestamp string, withGrantOption bool, roleId int64, objType objectType, objId, privilegeId int64) string {
	return fmt.Sprintf(updateRolePrivsFormat, userId, timestamp, withGrantOption, roleId, objType, objId, privilegeId)
}

func getSqlForInsertRolePrivs(roleId int64, roleName, objType string, objId, privilegeId int64, privilegeName, privilegeLevel string, operationUserId int64, grantedTime string, withGrantOption bool) string {
	return fmt.Sprintf(insertRolePrivsFormat, roleId, roleName, objType, objId, privilegeId, privilegeName, privilegeLevel, operationUserId, grantedTime, withGrantOption)
}

func getSqlForDeleteRolePrivs(roleId int64, objType string, objId, privilegeId int64, privilegeLevel string) string {
	return fmt.Sprintf(deleteRolePrivsFormat, roleId, objType, objId, privilegeId, privilegeLevel)
}

func getSqlForCheckWithGrantOptionForTableStarStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkWithGrantOptionForTableStarStar, objectTypeTable, roleId, privId, privilegeLevelStarStar)
}

func getSqlForCheckWithGrantOptionForTableDatabaseStar(ctx context.Context, roleId int64, privId PrivilegeType, dbName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkWithGrantOptionForTableDatabaseStar, objectTypeTable, roleId, privId, privilegeLevelDatabaseStar, dbName), nil
}

func getSqlForCheckWithGrantOptionForTableDatabaseTable(ctx context.Context, roleId int64, privId PrivilegeType, dbName string, tableName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName, tableName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkWithGrantOptionForTableDatabaseTable, objectTypeTable, roleId, privId, privilegeLevelDatabaseTable, dbName, tableName), nil
}

func getSqlForCheckWithGrantOptionForDatabaseStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkWithGrantOptionForDatabaseStar, objectTypeDatabase, roleId, privId, privilegeLevelStar)
}

func getSqlForCheckWithGrantOptionForDatabaseStarStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkWithGrantOptionForDatabaseStarStar, objectTypeDatabase, roleId, privId, privilegeLevelStarStar)
}

func getSqlForCheckWithGrantOptionForDatabaseDB(ctx context.Context, roleId int64, privId PrivilegeType, dbName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkWithGrantOptionForDatabaseDB, objectTypeDatabase, roleId, privId, privilegeLevelDatabase, dbName), nil
}

func getSqlForCheckWithGrantOptionForAccountStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkWithGrantOptionForAccountStar, objectTypeAccount, roleId, privId, privilegeLevelStarStar)
}

func getSqlForCheckRoleHasTableLevelPrivilege(ctx context.Context, roleId int64, privId PrivilegeType, dbName string, tableName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName, tableName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkRoleHasTableLevelPrivilegeFormat, objectTypeTable, roleId, privId, privilegeLevelDatabaseTable, privilegeLevelTable, dbName, tableName), nil
}

func getSqlForCheckRoleHasTableLevelForDatabaseStar(ctx context.Context, roleId int64, privId PrivilegeType, dbName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkRoleHasTableLevelForDatabaseStarFormat, objectTypeTable, roleId, privId, privilegeLevelDatabaseStar, privilegeLevelStar, dbName), nil
}

func getSqlForCheckRoleHasTableLevelForStarStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkRoleHasTableLevelForStarStarFormat, objectTypeTable, roleId, privId, privilegeLevelStarStar)
}

func getSqlForCheckRoleHasDatabaseLevelForStarStar(roleId int64, privId PrivilegeType, level privilegeLevelType) string {
	return fmt.Sprintf(checkRoleHasDatabaseLevelForStarStarFormat, objectTypeDatabase, roleId, privId, level)
}

func getSqlForCheckRoleHasDatabaseLevelForDatabase(ctx context.Context, roleId int64, privId PrivilegeType, dbName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkRoleHasDatabaseLevelForDatabaseFormat, objectTypeDatabase, roleId, privId, privilegeLevelDatabase, dbName), nil
}

func getSqlForCheckRoleHasAccountLevelForStar(roleId int64, privId PrivilegeType) string {
	return fmt.Sprintf(checkRoleHasAccountLevelForStarFormat, objectTypeAccount, roleId, privId, privilegeLevelStar)
}

func getSqlForgetUserRolesExpectPublicRole(pRoleId int, userId uint32) string {
	return fmt.Sprintf(getUserRolesExpectPublicRoleFormat, pRoleId, userId)
}

func getSqlForGetDbIdAndType(ctx context.Context, dbName string, checkNameValid bool, account_id uint64) (string, error) {
	if checkNameValid {
		err := inputNameIsInvalid(ctx, dbName)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(getDbIdAndTypFormat, dbName, account_id), nil
}

func getSqlForInsertIntoMoPubs(ctx context.Context, pubName, databaseName string, databaseId uint64, allTable bool, tableList, accountList string, owner, creator uint32, comment string, checkNameValid bool) (string, error) {
	if checkNameValid {
		err := inputNameIsInvalid(ctx, pubName, databaseName)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(insertIntoMoPubsFormat, pubName, databaseName, databaseId, allTable, tableList, accountList, owner, creator, comment), nil
}
func getSqlForGetPubInfo(ctx context.Context, pubName string, checkNameValid bool) (string, error) {
	if checkNameValid {
		err := inputNameIsInvalid(ctx, pubName)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(getPubInfoFormat, pubName), nil
}

func getSqlForUpdatePubInfo(ctx context.Context, pubName string, accountList string, comment string, checkNameValid bool) (string, error) {
	if checkNameValid {
		err := inputNameIsInvalid(ctx, pubName)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(updatePubInfoFormat, accountList, comment, pubName), nil
}

func getSqlForDropPubInfo(ctx context.Context, pubName string, checkNameValid bool) (string, error) {
	if checkNameValid {
		err := inputNameIsInvalid(ctx, pubName)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(dropPubFormat, pubName), nil
}

func getSqlForDbPubCount(ctx context.Context, dbName string) (string, error) {

	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(getDbPubCountFormat, dbName), nil
}

func getSqlForCheckDatabase(ctx context.Context, dbName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkDatabaseFormat, dbName), nil
}

func getSqlForCheckDatabaseTable(ctx context.Context, dbName, tableName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName, tableName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(checkDatabaseTableFormat, dbName, tableName), nil
}

func getSqlForDeleteRole(roleId int64) []string {
	return []string{
		fmt.Sprintf(deleteRoleFromMoRoleFormat, roleId),
		fmt.Sprintf(deleteRoleFromMoUserGrantFormat, roleId),
		fmt.Sprintf(deleteRoleFromMoRoleGrantFormat, roleId, roleId),
		fmt.Sprintf(deleteRoleFromMoRolePrivsFormat, roleId),
	}
}

func getSqlForDropAccount() []string {
	return dropSqls
}

func getSqlForDeleteUser(userId int64) []string {
	return []string{
		fmt.Sprintf(deleteUserFromMoUserFormat, userId),
		fmt.Sprintf(deleteUserFromMoUserGrantFormat, userId),
	}
}

func getSqlForDeleteMysqlCompatbilityMode(dtname string) string {
	return fmt.Sprintf(deleteMysqlCompatbilityModeFormat, dtname)
}

func getSqlForGetSystemVariableValueWithDatabase(dtname, variable_name string) string {
	return fmt.Sprintf(getSystemVariableValueWithDatabaseFormat, dtname, variable_name)
}

func getSystemVariablesWithAccount(accountId uint64) string {
	return fmt.Sprintf(getSystemVariablesWithAccountFromat, accountId)
}

// getSqlForGetSystemVariableValueWithAccount will get sql for get variable value with specific account
func getSqlForGetSystemVariableValueWithAccount(accountId uint64, varName string) string {
	return fmt.Sprintf(getSystemVariableValueWithAccountFromat, accountId, varName)
}

// getSqlForUpdateSystemVariableValue returns a SQL query to update the value of a system variable for a given account.
func getSqlForUpdateSystemVariableValue(varValue string, accountId uint64, varName string) string {
	return fmt.Sprintf(updateSystemVariableValueFormat, varValue, accountId, varName)
}

func getSqlForupdateConfigurationByDbNameAndAccountName(ctx context.Context, varValue, accountName, dbName, varName string) (string, error) {
	err := inputNameIsInvalid(ctx, dbName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updateConfigurationByDbNameAndAccountNameFormat, varValue, accountName, dbName, varName), nil
}

func getSqlForupdateConfigurationByAccount(ctx context.Context, varValue, accountName, varName string) (string, error) {
	err := inputNameIsInvalid(ctx, accountName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(updateConfigurationByAccountNameFormat, varValue, accountName, varName), nil
}

func getSqlForSpBody(ctx context.Context, name string, db string) (string, error) {
	return fmt.Sprintf(fetchSqlOfSpFormat, name, db), nil
}

// isClusterTable decides a table is the index table or not
func isIndexTable(name string) bool {
	return strings.HasPrefix(name, catalog.IndexTableNamePrefix)
}

// isClusterTable decides a table is the cluster table or not
func isClusterTable(dbName, name string) bool {
	if dbName == moCatalog {
		//if it is neither among the tables nor the index table,
		//it is the cluster table.
		if _, ok := predefinedTables[name]; !ok && !isIndexTable(name) {
			return true
		}
	}
	return false
}

// getSqlForGrantOwnershipOnDatabase get the sql for grant ownership on database
func getSqlForGrantOwnershipOnDatabase(dbName, roleName string) string {
	return fmt.Sprintf(grantOwnershipOnDatabaseFormat, dbName, roleName)
}

// getSqlForGrantOwnershipOnTable get the sql for grant ownership on database
func getSqlForGrantOwnershipOnTable(dbName, tbName, roleName string) string {
	return fmt.Sprintf(grantOwnershipOnTableFormat, dbName, tbName, roleName)
}

// getSqlForGetOwnerOfDatabase get the sql for get the owner of the database
func getSqlForGetOwnerOfDatabase(dbName string) string {
	return fmt.Sprintf(getOwnerOfDatabaseFormat, dbName)
}

// getSqlForGetOwnerOfTable get the sql for get the owner of the table
func getSqlForGetOwnerOfTable(dbName, tbName string) string {
	return fmt.Sprintf(getOwnerOfTableFormat, dbName, tbName)
}

// getSqlForGetRolesOfCurrentUser get the sql for get the roles of the user
func getSqlForGetRolesOfCurrentUser(userId int64) string {
	return fmt.Sprintf(getRolesOfCurrentUserFormat, userId)
}

// getSqlForCheckProcedureExistence get the sql for check the procedure exist or not
func getSqlForCheckProcedureExistence(pdName, dbName string) string {
	return fmt.Sprintf(checkProcedureExistence, pdName, dbName)
}

func isBannedDatabase(dbName string) bool {
	_, ok := bannedCatalogDatabases[dbName]
	return ok
}

func isBannedPrivilege(priv PrivilegeType) bool {
	_, ok := bannedPrivileges[priv]
	return ok
}

type specialTag int

const (
	specialTagNone            specialTag = 0
	specialTagAdmin           specialTag = 1
	specialTagWithGrantOption specialTag = 2
	specialTagOwnerOfObject   specialTag = 4
)

type privilegeKind int

const (
	privilegeKindGeneral privilegeKind = iota //as same as definition in the privilegeEntriesMap
	privilegeKindInherit                      //General + with_grant_option
	privilegeKindSpecial                      //no obj_type,obj_id,privilege_level. only needs (MOADMIN / ACCOUNTADMIN, with_grant_option, owner of object)
	privilegeKindNone                         //does not need any privilege
)

type clusterTableOperationType int

const (
	clusterTableNone clusterTableOperationType = iota
	clusterTableCreate
	clusterTableSelect //read only
	clusterTableModify //include insert,update,delete
	clusterTableDrop
)

type privilege struct {
	kind privilegeKind
	//account: the privilege can be defined before constructing the plan.
	//database: (do not need the database_id) the privilege can be defined before constructing the plan.
	//table: need table id. the privilege can be defined after constructing the plan.
	//function: need function id ?
	objType objectType
	entries []privilegeEntry
	special specialTag
	//the statement writes the database or table directly like drop database and table
	writeDatabaseAndTableDirectly bool
	//operate the cluster table
	isClusterTable bool
	//operation on cluster table,
	clusterTableOperation clusterTableOperationType
}

func (p *privilege) objectType() objectType {
	return p.objType
}

func (p *privilege) privilegeKind() privilegeKind {
	return p.kind
}

type privilegeEntryType int

const (
	privilegeEntryTypeGeneral  privilegeEntryType = iota
	privilegeEntryTypeCompound                    //multi privileges take effect together
)

// privilegeItem is the item for in the compound entry
type privilegeItem struct {
	privilegeTyp          PrivilegeType
	role                  *tree.Role
	users                 []*tree.User
	dbName                string
	tableName             string
	isClusterTable        bool
	clusterTableOperation clusterTableOperationType
}

// compoundEntry is the entry has multi privilege items
type compoundEntry struct {
	items []privilegeItem
}

// privilegeEntry denotes the entry of the privilege that appears in the table mo_role_privs
type privilegeEntry struct {
	privilegeId PrivilegeType
	//the predefined privilege level for the privilege.
	//it is not always the same as the one in the runtime.
	privilegeLevel  privilegeLevelType
	objType         objectType
	objId           int
	withGrantOption bool
	//for object type table
	databaseName      string
	tableName         string
	privilegeEntryTyp privilegeEntryType
	compound          *compoundEntry
}

var (
	//initial privilege entries
	privilegeEntriesMap = map[PrivilegeType]privilegeEntry{
		PrivilegeTypeCreateAccount:     {PrivilegeTypeCreateAccount, privilegeLevelStar, objectTypeAccount, objectIDAll, false, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropAccount:       {PrivilegeTypeDropAccount, privilegeLevelStar, objectTypeAccount, objectIDAll, false, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterAccount:      {PrivilegeTypeAlterAccount, privilegeLevelStar, objectTypeAccount, objectIDAll, false, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateUser:        {PrivilegeTypeCreateUser, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropUser:          {PrivilegeTypeDropUser, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterUser:         {PrivilegeTypeAlterUser, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateRole:        {PrivilegeTypeCreateRole, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropRole:          {PrivilegeTypeDropRole, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterRole:         {PrivilegeTypeAlterRole, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateDatabase:    {PrivilegeTypeCreateDatabase, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropDatabase:      {PrivilegeTypeDropDatabase, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeShowDatabases:     {PrivilegeTypeShowDatabases, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeConnect:           {PrivilegeTypeConnect, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeManageGrants:      {PrivilegeTypeManageGrants, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAccountAll:        {PrivilegeTypeAccountAll, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAccountOwnership:  {PrivilegeTypeAccountOwnership, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeUserOwnership:     {PrivilegeTypeUserOwnership, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeRoleOwnership:     {PrivilegeTypeRoleOwnership, privilegeLevelStar, objectTypeAccount, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeShowTables:        {PrivilegeTypeShowTables, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateObject:      {PrivilegeTypeCreateObject, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateTable:       {PrivilegeTypeCreateTable, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeCreateView:        {PrivilegeTypeCreateView, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropObject:        {PrivilegeTypeDropObject, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropTable:         {PrivilegeTypeDropTable, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDropView:          {PrivilegeTypeDropView, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterObject:       {PrivilegeTypeAlterObject, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterTable:        {PrivilegeTypeAlterTable, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeAlterView:         {PrivilegeTypeAlterView, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDatabaseAll:       {PrivilegeTypeDatabaseAll, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDatabaseOwnership: {PrivilegeTypeDatabaseOwnership, privilegeLevelStar, objectTypeDatabase, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeSelect:            {PrivilegeTypeSelect, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeInsert:            {PrivilegeTypeInsert, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeUpdate:            {PrivilegeTypeUpdate, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeTruncate:          {PrivilegeTypeTruncate, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeDelete:            {PrivilegeTypeDelete, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeReference:         {PrivilegeTypeReference, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeIndex:             {PrivilegeTypeIndex, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeTableAll:          {PrivilegeTypeTableAll, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeTableOwnership:    {PrivilegeTypeTableOwnership, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeExecute:           {PrivilegeTypeExecute, privilegeLevelRoutine, objectTypeFunction, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
		PrivilegeTypeValues:            {PrivilegeTypeValues, privilegeLevelStarStar, objectTypeTable, objectIDAll, true, "", "", privilegeEntryTypeGeneral, nil},
	}

	//the initial entries of mo_role_privs for the role 'moadmin'
	entriesOfMoAdminForMoRolePrivsFor = []PrivilegeType{
		PrivilegeTypeCreateAccount,
		PrivilegeTypeDropAccount,
		PrivilegeTypeAlterAccount,
		PrivilegeTypeCreateUser,
		PrivilegeTypeDropUser,
		PrivilegeTypeAlterUser,
		PrivilegeTypeCreateRole,
		PrivilegeTypeDropRole,
		PrivilegeTypeCreateDatabase,
		PrivilegeTypeDropDatabase,
		PrivilegeTypeShowDatabases,
		PrivilegeTypeConnect,
		PrivilegeTypeManageGrants,
		PrivilegeTypeAccountAll,
		PrivilegeTypeShowTables,
		PrivilegeTypeCreateTable,
		PrivilegeTypeDropTable,
		PrivilegeTypeAlterTable,
		PrivilegeTypeCreateView,
		PrivilegeTypeDropView,
		PrivilegeTypeAlterView,
		PrivilegeTypeDatabaseAll,
		PrivilegeTypeDatabaseOwnership,
		PrivilegeTypeSelect,
		PrivilegeTypeInsert,
		PrivilegeTypeUpdate,
		PrivilegeTypeTruncate,
		PrivilegeTypeDelete,
		PrivilegeTypeReference,
		PrivilegeTypeIndex,
		PrivilegeTypeTableAll,
		PrivilegeTypeTableOwnership,
		PrivilegeTypeValues,
	}

	//the initial entries of mo_role_privs for the role 'accountadmin'
	entriesOfAccountAdminForMoRolePrivsFor = []PrivilegeType{
		PrivilegeTypeCreateUser,
		PrivilegeTypeDropUser,
		PrivilegeTypeAlterUser,
		PrivilegeTypeCreateRole,
		PrivilegeTypeDropRole,
		PrivilegeTypeCreateDatabase,
		PrivilegeTypeDropDatabase,
		PrivilegeTypeShowDatabases,
		PrivilegeTypeConnect,
		PrivilegeTypeManageGrants,
		PrivilegeTypeAccountAll,
		PrivilegeTypeShowTables,
		PrivilegeTypeCreateTable,
		PrivilegeTypeDropTable,
		PrivilegeTypeAlterTable,
		PrivilegeTypeCreateView,
		PrivilegeTypeDropView,
		PrivilegeTypeAlterView,
		PrivilegeTypeDatabaseAll,
		PrivilegeTypeDatabaseOwnership,
		PrivilegeTypeSelect,
		PrivilegeTypeInsert,
		PrivilegeTypeUpdate,
		PrivilegeTypeTruncate,
		PrivilegeTypeDelete,
		PrivilegeTypeReference,
		PrivilegeTypeIndex,
		PrivilegeTypeTableAll,
		PrivilegeTypeTableOwnership,
		PrivilegeTypeValues,
	}

	//the initial entries of mo_role_privs for the role 'public'
	entriesOfPublicForMoRolePrivsFor = []PrivilegeType{
		PrivilegeTypeConnect,
	}
)

type verifiedRoleType int

const (
	roleType verifiedRoleType = iota
	userType
)

// privilegeCache cache privileges on table
type privilegeCache struct {
	//For objectType table
	//For objectType table *, *.*
	storeForTable [int(privilegeLevelEnd)]btree.Set[PrivilegeType]
	//For objectType table database.*
	storeForTable2 btree.Map[string, *btree.Set[PrivilegeType]]
	//For objectType table database.table , table
	storeForTable3 btree.Map[string, *btree.Map[string, *btree.Set[PrivilegeType]]]

	//For objectType database *, *.*
	storeForDatabase [int(privilegeLevelEnd)]btree.Set[PrivilegeType]
	//For objectType database
	storeForDatabase2 btree.Map[string, *btree.Set[PrivilegeType]]
	//For objectType account *
	storeForAccount [int(privilegeLevelEnd)]btree.Set[PrivilegeType]
	total           atomic.Uint64
	hit             atomic.Uint64
}

// has checks the cache has privilege on a table
func (pc *privilegeCache) has(objTyp objectType, plt privilegeLevelType, dbName, tableName string, priv PrivilegeType) bool {
	pc.total.Add(1)
	privSet := pc.getPrivilegeSet(objTyp, plt, dbName, tableName)
	if privSet != nil && privSet.Contains(priv) {
		pc.hit.Add(1)
		return true
	}
	return false
}

func (pc *privilegeCache) getPrivilegeSet(objTyp objectType, plt privilegeLevelType, dbName, tableName string) *btree.Set[PrivilegeType] {
	switch objTyp {
	case objectTypeTable:
		switch plt {
		case privilegeLevelStarStar, privilegeLevelStar:
			return &pc.storeForTable[plt]
		case privilegeLevelDatabaseStar:
			dbStore, ok1 := pc.storeForTable2.Get(dbName)
			if !ok1 {
				dbStore = &btree.Set[PrivilegeType]{}
				pc.storeForTable2.Set(dbName, dbStore)
			}
			return dbStore
		case privilegeLevelDatabaseTable, privilegeLevelTable:
			tableStore, ok1 := pc.storeForTable3.Get(dbName)
			if !ok1 {
				tableStore = &btree.Map[string, *btree.Set[PrivilegeType]]{}
				pc.storeForTable3.Set(dbName, tableStore)
			}
			privSet, ok2 := tableStore.Get(tableName)
			if !ok2 {
				privSet = &btree.Set[PrivilegeType]{}
				tableStore.Set(tableName, privSet)
			}
			return privSet
		default:
			return nil
		}
	case objectTypeDatabase:
		switch plt {
		case privilegeLevelStar, privilegeLevelStarStar:
			return &pc.storeForDatabase[plt]
		case privilegeLevelDatabase:
			dbStore, ok1 := pc.storeForDatabase2.Get(dbName)
			if !ok1 {
				dbStore = &btree.Set[PrivilegeType]{}
				pc.storeForDatabase2.Set(dbName, dbStore)
			}
			return dbStore
		default:
			return nil
		}
	case objectTypeAccount:
		return &pc.storeForAccount[plt]
	default:
		return nil
	}

}

// set replaces the privileges by new ones
func (pc *privilegeCache) set(objTyp objectType, plt privilegeLevelType, dbName, tableName string, priv ...PrivilegeType) {
	privSet := pc.getPrivilegeSet(objTyp, plt, dbName, tableName)
	if privSet != nil {
		privSet.Clear()
		for _, p := range priv {
			privSet.Insert(p)
		}
	}
}

// add puts the privileges without replacing existed ones
func (pc *privilegeCache) add(objTyp objectType, plt privilegeLevelType, dbName, tableName string, priv ...PrivilegeType) {
	privSet := pc.getPrivilegeSet(objTyp, plt, dbName, tableName)
	if privSet != nil {
		for _, p := range priv {
			privSet.Insert(p)
		}
	}
}

// invalidate makes the cache empty
func (pc *privilegeCache) invalidate() {
	if pc == nil {
		return
	}
	//total := pc.total.Swap(0)
	//hit := pc.hit.Swap(0)
	for i := privilegeLevelStar; i < privilegeLevelEnd; i++ {
		pc.storeForTable[i].Clear()
		pc.storeForDatabase[i].Clear()
		pc.storeForAccount[i].Clear()
	}
	pc.storeForTable2.Clear()
	pc.storeForTable3.Clear()
	pc.storeForDatabase2.Clear()
	//ratio := float64(0)
	//if total == 0 {
	//	ratio = 0
	//} else {
	//	ratio = float64(hit) / float64(total)
	//}
	//logutil.Debugf("-->hit %d total %d ratio %f", hit, total, ratio)
}

// verifiedRole holds the role info that has been checked
type verifiedRole struct {
	typ         verifiedRoleType
	name        string
	id          int64
	userIsAdmin bool
}

// verifyRoleFunc gets result set from mo_role_grant or mo_user_grant
func verifyRoleFunc(ctx context.Context, bh BackgroundExec, sql, name string, typ verifiedRoleType) (*verifiedRole, error) {
	var err error
	var erArray []ExecResult
	var roleId int64
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, err
	}

	if execResultArrayHasData(erArray) {
		roleId, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return nil, err
		}
		return &verifiedRole{typ, name, roleId, false}, nil
	}
	return nil, nil
}

// userIsAdministrator checks the user is the administrator
func userIsAdministrator(ctx context.Context, bh BackgroundExec, userId int64, account *TenantInfo) (bool, error) {
	var err error
	var erArray []ExecResult
	var sql string
	if account.IsSysTenant() {
		sql, err = getSqlForRoleOfUser(ctx, userId, moAdminRoleName)
	} else {
		sql, err = getSqlForRoleOfUser(ctx, userId, accountAdminRoleName)
	}
	if err != nil {
		return false, err
	}

	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return false, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}

	if execResultArrayHasData(erArray) {
		return true, nil
	}
	return false, nil
}

type visitTag int

const (
	vtUnVisited visitTag = 0
	vtVisited   visitTag = 1
	vtVisiting  visitTag = -1
)

// edge <from,to> in the graph
type edge struct {
	from    int64
	to      int64
	invalid bool
}

func (e *edge) isInvalid() bool {
	return e.invalid
}

func (e *edge) setInvalid() {
	e.invalid = true
}

// graph the acyclic graph
type graph struct {
	edges    []*edge
	vertexes map[int64]int
	adjacent map[int64][]int
}

func NewGraph() *graph {
	return &graph{
		vertexes: make(map[int64]int),
		adjacent: make(map[int64][]int),
	}
}

// addEdge adds the directed edge <from,to> into the graph
func (g *graph) addEdge(from, to int64) int {
	edgeId := len(g.edges)
	g.edges = append(g.edges, &edge{from, to, false})
	g.adjacent[from] = append(g.adjacent[from], edgeId)
	g.vertexes[from] = 0
	g.vertexes[to] = 0
	return edgeId
}

// removeEdge removes the directed edge (edgeId) from the graph
func (g *graph) removeEdge(edgeId int) {
	e := g.getEdge(edgeId)
	e.setInvalid()
}

func (g *graph) getEdge(eid int) *edge {
	return g.edges[eid]
}

// dfs use the toposort to check the loop
func (g *graph) toposort(u int64, visited map[int64]visitTag) bool {
	visited[u] = vtVisiting
	//loop on adjacent vertex
	for _, eid := range g.adjacent[u] {
		e := g.getEdge(eid)
		if e.isInvalid() {
			continue
		}
		if visited[e.to] == vtVisiting { //find the loop in the vertex
			return false
		} else if visited[e.to] == vtUnVisited && !g.toposort(e.to, visited) { //find the loop in the adjacent vertexes
			return false
		}
	}
	visited[u] = vtVisited
	return true
}

// hasLoop checks the loop
func (g *graph) hasLoop(start int64) bool {
	visited := make(map[int64]visitTag)
	for v := range g.vertexes {
		visited[v] = vtUnVisited
	}

	return !g.toposort(start, visited)
}

func inputNameIsInvalid(ctx context.Context, inputs ...string) error {
	for _, input := range inputs {
		for _, t := range input {
			if t == ' ' || t == '\t' || t == '`' || t == '"' || t == '\'' {
				return moerr.NewInternalError(ctx, `invalid input`)
			}
		}
	}
	return nil
}

// nameIsInvalid checks the name of user/role is valid or not
func nameIsInvalid(name string) bool {
	s := strings.TrimSpace(name)
	if len(s) == 0 {
		return true
	}
	return strings.Contains(s, ":") || strings.Contains(s, "#")
}
func accountNameIsInvalid(name string) bool {
	s := strings.TrimSpace(name)
	if len(s) == 0 {
		return true
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			continue
		case c >= 'a' && c <= 'z':
			continue
		case c >= 'A' && c <= 'Z':
			continue
		case c == '_' || c == '-':
			continue
		default:
			return true
		}
	}
	return false
}

// normalizeName normalizes and checks the name
func normalizeName(ctx context.Context, name string) (string, error) {
	s := strings.TrimSpace(name)
	if nameIsInvalid(s) {
		return "", moerr.NewInternalError(ctx, `the name "%s" is invalid`, name)
	}
	return s, nil
}

func normalizeNameOfAccount(ctx context.Context, ca *tree.CreateAccount) error {
	s := strings.TrimSpace(ca.Name)
	if len(s) == 0 {
		return moerr.NewInternalError(ctx, `the name "%s" is invalid`, ca.Name)
	}
	if accountNameIsInvalid(s) {
		return moerr.NewInternalError(ctx, `the name "%s" is invalid`, ca.Name)
	}
	ca.Name = s
	return nil
}

// normalizeNameOfRole normalizes the name
func normalizeNameOfRole(ctx context.Context, role *tree.Role) error {
	var err error
	role.UserName, err = normalizeName(ctx, role.UserName)
	return err
}

// normalizeNamesOfRoles normalizes the names and checks them
func normalizeNamesOfRoles(ctx context.Context, roles []*tree.Role) error {
	var err error
	for i := 0; i < len(roles); i++ {
		err = normalizeNameOfRole(ctx, roles[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// normalizeNameOfUser normalizes the name
func normalizeNameOfUser(ctx context.Context, user *tree.User) error {
	var err error
	user.Username, err = normalizeName(ctx, user.Username)
	return err
}

// normalizeNamesOfUsers normalizes the names and checks them
func normalizeNamesOfUsers(ctx context.Context, users []*tree.User) error {
	var err error
	for i := 0; i < len(users); i++ {
		err = normalizeNameOfUser(ctx, users[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// finishTxn cleanup the transaction started by the BEGIN statement.
// If the transaction is successful, commit it. Otherwise, rollback it.
// err == nil means there is no failure during the transaction execution.
// so the transaction is successful. Else, it is failed.
// !!!Note: if the transaction is not started by the BEGIN statement, it
// has been COMMIT or ROLLBACK already. It is wrong to COMMIT or ROLLBACK again, obviously.
// It is wrong to call this function to commit or rollback the transaction.
func finishTxn(ctx context.Context, bh BackgroundExec, err error) error {
	rollbackTxn := func() error {
		//ROLLBACK the transaction
		rbErr := bh.Exec(ctx, "rollback;")
		if rbErr != nil {
			//if ROLLBACK failed, return the COMMIT error with the input err also
			return errors.Join(rbErr, err)
		}
		return err
	}
	if err == nil {
		//normal COMMIT the transaction
		err = bh.Exec(ctx, "commit;")
		if err != nil {
			//if COMMIT failed, ROLLBACK the transaction
			return rollbackTxn()
		}
		return err
	}
	return rollbackTxn()
}

func doAlterUser(ctx context.Context, ses *Session, au *tree.AlterUser) (err error) {
	var sql string
	var vr *verifiedRole
	var user *tree.User
	var userName string
	var hostName string
	var password string
	var erArray []ExecResult
	var encryption string
	account := ses.GetTenantInfo()
	currentUser := account.User

	//1.authenticate the actions
	if au.Role != nil {
		return moerr.NewInternalError(ctx, "not support alter role")
	}
	if au.MiscOpt != nil {
		return moerr.NewInternalError(ctx, "not support password or lock operation")
	}
	if au.CommentOrAttribute.Exist {
		return moerr.NewInternalError(ctx, "not support alter comment or attribute")
	}
	if len(au.Users) != 1 {
		return moerr.NewInternalError(ctx, "can only alter one user at a time")
	}

	err = normalizeNamesOfUsers(ctx, au.Users)
	if err != nil {
		return err
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	user = au.Users[0]
	userName = user.Username
	hostName = user.Hostname
	password = user.AuthOption.Str
	if len(password) == 0 {
		return moerr.NewInternalError(ctx, "password is empty string")
	}
	//put it into the single transaction
	err = bh.Exec(ctx, "begin")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	if user.AuthOption == nil {
		return moerr.NewInternalError(ctx, "Operation ALTER USER failed for '%s'@'%s', alter Auth is nil", userName, hostName)
	}

	if user.AuthOption.Typ != tree.AccountIdentifiedByPassword {
		return moerr.NewInternalError(ctx, "Operation ALTER USER failed for '%s'@'%s', only support alter Auth by identified by", userName, hostName)
	}

	//check the user exists or not
	sql, err = getSqlForPasswordOfUser(ctx, userName)
	if err != nil {
		return err
	}
	vr, err = verifyRoleFunc(ctx, bh, sql, userName, roleType)
	if err != nil {
		return err
	}

	if vr == nil {
		//If Exists :
		// false : return an error
		// true : return and  do nothing
		if !au.IfExists {
			return moerr.NewInternalError(ctx, "Operation ALTER USER failed for '%s'@'%s', user does't exist", user.Username, user.Hostname)
		} else {
			return err
		}
	}

	//if the user is admin user with the role moadmin or accountadmin,
	//the user can be altered
	//otherwise only general user can alter itself
	if account.IsSysTenant() {
		sql, err = getSqlForCheckUserHasRole(ctx, currentUser, moAdminRoleID)
	} else {
		sql, err = getSqlForCheckUserHasRole(ctx, currentUser, accountAdminRoleID)
	}
	if err != nil {
		return err
	}

	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}

	//encryption the password
	encryption = HashPassWord(password)

	if execResultArrayHasData(erArray) {
		sql, err = getSqlForUpdatePasswordOfUser(ctx, encryption, userName)
		if err != nil {
			return err
		}
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	} else {
		if currentUser != userName {
			return moerr.NewInternalError(ctx, "Operation ALTER USER failed for '%s'@'%s', don't have the privilege to alter", userName, hostName)
		}
		sql, err = getSqlForUpdatePasswordOfUser(ctx, encryption, userName)
		if err != nil {
			return err
		}
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	}
	return err
}
func doAlterAccount(ctx context.Context, ses *Session, aa *tree.AlterAccount) (err error) {
	var sql string
	var erArray []ExecResult
	var targetAccountId uint64
	var version uint64
	var accountExist bool
	account := ses.GetTenantInfo()
	if !(account.IsSysTenant() && account.IsMoAdminRole()) {
		return moerr.NewInternalError(ctx, "tenant %s user %s role %s do not have the privilege to alter the account",
			account.GetTenant(), account.GetUser(), account.GetDefaultRole())
	}

	optionBits := uint8(0)
	if aa.AuthOption.Exist {
		optionBits |= 1
	}
	if aa.StatusOption.Exist {
		optionBits |= 1 << 1
	}
	if aa.Comment.Exist {
		optionBits |= 1 << 2
	}
	optionCount := bits.OnesCount8(optionBits)
	if optionCount == 0 {
		return moerr.NewInternalError(ctx, "at least one option at a time")
	}
	if optionCount > 1 {
		return moerr.NewInternalError(ctx, "at most one option at a time")
	}

	//normalize the name
	aa.Name, err = normalizeName(ctx, aa.Name)
	if err != nil {
		return err
	}

	if aa.AuthOption.Exist {
		aa.AuthOption.AdminName, err = normalizeName(ctx, aa.AuthOption.AdminName)
		if err != nil {
			return err
		}
		if aa.AuthOption.IdentifiedType.Typ != tree.AccountIdentifiedByPassword {
			return moerr.NewInternalError(ctx, "only support identified by password")
		}

		if len(aa.AuthOption.IdentifiedType.Str) == 0 {
			err = moerr.NewInternalError(ctx, "password is empty string")
			return err
		}
	}

	if aa.StatusOption.Exist {
		//SYS account can not be suspended
		if isSysTenant(aa.Name) {
			return moerr.NewInternalError(ctx, "account sys can not be suspended")
		}
	}

	alterAccountFunc := func() error {
		bh := ses.GetBackgroundExec(ctx)
		defer bh.Close()

		err = bh.Exec(ctx, "begin")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return err
		}

		//step 1: check account exists or not
		//get accountID
		sql, err = getSqlForCheckTenant(ctx, aa.Name)
		if err != nil {
			return err
		}
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
				targetAccountId, err = erArray[0].GetUint64(ctx, i, 0)
				if err != nil {
					return err
				}
				version, err = erArray[0].GetUint64(ctx, i, 3)
				if err != nil {
					return err
				}
			}
			accountExist = true
		} else {
			//IfExists :
			// false : return an error
			// true : skip and do nothing
			if !aa.IfExists {
				return moerr.NewInternalError(ctx, "there is no account %s", aa.Name)
			}
		}

		if accountExist {
			//Option 1: alter the password of admin for the account
			if aa.AuthOption.Exist {
				//!!!NOTE!!!:switch into the target account's context, then update the table mo_user.
				accountCtx := context.WithValue(ctx, defines.TenantIDKey{}, uint32(targetAccountId))

				//1, check the admin exists or not
				sql, err = getSqlForPasswordOfUser(ctx, aa.AuthOption.AdminName)
				if err != nil {
					return err
				}
				bh.ClearExecResultSet()
				err = bh.Exec(accountCtx, sql)
				if err != nil {
					return err
				}

				erArray, err = getResultSet(accountCtx, bh)
				if err != nil {
					return err
				}

				if !execResultArrayHasData(erArray) {
					return moerr.NewInternalError(accountCtx, "there is no user %s", aa.AuthOption.AdminName)
				}

				//2, update the password
				//encryption the password
				encryption := HashPassWord(aa.AuthOption.IdentifiedType.Str)
				sql, err = getSqlForUpdatePasswordOfUser(ctx, encryption, aa.AuthOption.AdminName)
				if err != nil {
					return err
				}
				bh.ClearExecResultSet()
				err = bh.Exec(accountCtx, sql)
				if err != nil {
					return err
				}
			}

			//Option 2: alter the comment of the account
			if aa.Comment.Exist {
				sql, err = getSqlForUpdateCommentsOfAccount(ctx, aa.Comment.Comment, aa.Name)
				if err != nil {
					return err
				}
				bh.ClearExecResultSet()
				err = bh.Exec(ctx, sql)
				if err != nil {
					return err
				}
			}

			//Option 3: suspend or resume the account
			if aa.StatusOption.Exist {
				if aa.StatusOption.Option == tree.AccountStatusSuspend {
					sql, err = getSqlForUpdateStatusOfAccount(ctx, aa.StatusOption.Option.String(), types.CurrentTimestamp().String2(time.UTC, 0), aa.Name)
					if err != nil {
						return err
					}
					bh.ClearExecResultSet()
					err = bh.Exec(ctx, sql)
					if err != nil {
						return err
					}
				} else if aa.StatusOption.Option == tree.AccountStatusOpen {
					sql, err = getSqlForUpdateStatusAndVersionOfAccount(ctx, aa.StatusOption.Option.String(), types.CurrentTimestamp().String2(time.UTC, 0), aa.Name, (version+1)%math.MaxUint64)
					if err != nil {
						return err
					}
					bh.ClearExecResultSet()
					err = bh.Exec(ctx, sql)
					if err != nil {
						return err
					}
				}
			}
		}
		return err
	}

	err = alterAccountFunc()
	if err != nil {
		return err
	}

	//if alter account suspend, add the account to kill queue
	if accountExist {
		if aa.StatusOption.Exist && aa.StatusOption.Option == tree.AccountStatusSuspend {
			ses.getRoutineManager().accountRoutine.enKillQueue(int64(targetAccountId), version)
		}
	}

	return err
}

// doSetSecondaryRoleAll set the session role of the user with smallness role_id
func doSetSecondaryRoleAll(ctx context.Context, ses *Session) (err error) {
	var sql string
	var userId uint32
	var erArray []ExecResult
	var roleId int64
	var roleName string

	account := ses.GetTenantInfo()
	// get current user_id
	userId = account.GetUserID()

	// init role_id and role_name
	roleId = publicRoleID
	roleName = publicRoleName

	// step1:get all roles expect public
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	sql = getSqlForgetUserRolesExpectPublicRole(publicRoleID, userId)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}
	if execResultArrayHasData(erArray) {
		roleId, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return err
		}

		roleName, err = erArray[0].GetString(ctx, 0, 1)
		if err != nil {
			return err
		}
	}

	// step2 : switch the default role and role id;
	account.SetDefaultRoleID(uint32(roleId))
	account.SetDefaultRole(roleName)

	return err
}

// doSwitchRole accomplishes the Use Role and Use Secondary Role statement
func doSwitchRole(ctx context.Context, ses *Session, sr *tree.SetRole) (err error) {
	var sql string
	var erArray []ExecResult
	var roleId int64

	account := ses.GetTenantInfo()

	if sr.SecondaryRole {
		//use secondary role all or none
		switch sr.SecondaryRoleType {
		case tree.SecondaryRoleTypeAll:
			doSetSecondaryRoleAll(ctx, ses)
			account.SetUseSecondaryRole(true)
		case tree.SecondaryRoleTypeNone:
			account.SetUseSecondaryRole(false)
		}
	} else if sr.Role != nil {
		err = normalizeNameOfRole(ctx, sr.Role)
		if err != nil {
			return err
		}

		//step1 : check the role exists or not;

		switchRoleFunc := func() error {
			bh := ses.GetBackgroundExec(ctx)
			defer bh.Close()

			err = bh.Exec(ctx, "begin;")
			defer func() {
				err = finishTxn(ctx, bh, err)
			}()
			if err != nil {
				return err
			}

			sql, err = getSqlForRoleIdOfRole(ctx, sr.Role.UserName)
			if err != nil {
				return err
			}
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}
			if execResultArrayHasData(erArray) {
				roleId, err = erArray[0].GetInt64(ctx, 0, 0)
				if err != nil {
					return err
				}
			} else {
				return moerr.NewInternalError(ctx, "there is no role %s", sr.Role.UserName)
			}

			//step2 : check the role has been granted to the user or not
			sql = getSqlForCheckUserGrant(roleId, int64(account.GetUserID()))
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}

			if !execResultArrayHasData(erArray) {
				return moerr.NewInternalError(ctx, "the role %s has not be granted to the user %s", sr.Role.UserName, account.GetUser())
			}
			return err
		}

		err = switchRoleFunc()
		if err != nil {
			return err
		}

		//step3 : switch the default role and role id;
		account.SetDefaultRoleID(uint32(roleId))
		account.SetDefaultRole(sr.Role.UserName)
		//then, reset secondary role to none
		account.SetUseSecondaryRole(false)

		return err
	}

	return err
}

func getSubscriptionMeta(ctx context.Context, dbName string, ses *Session, txn TxnOperator) (*plan.SubscriptionMeta, error) {
	dbMeta, err := ses.GetParameterUnit().StorageEngine.Database(ctx, dbName, txn)
	if err != nil {
		return nil, err
	}

	if dbMeta.IsSubscription(ctx) {
		if sub, err := checkSubscriptionValid(ctx, ses, dbMeta.GetCreateSql(ctx)); err != nil {
			return nil, err
		} else {
			return sub, nil
		}
	}
	return nil, nil
}

func isSubscriptionValid(accountList string, accName string) bool {
	if accountList == "all" {
		return true
	}
	return strings.Contains(accountList, accName)
}

func checkSubscriptionValidCommon(ctx context.Context, ses *Session, subName, accName, pubName string) (subs *plan.SubscriptionMeta, err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()
	var (
		sql, accStatus, accountList, databaseName string
		erArray                                   []ExecResult
		tenantInfo                                *TenantInfo
		accId                                     int64
		newCtx                                    context.Context
		tenantName                                string
	)

	tenantInfo = ses.GetTenantInfo()
	if tenantInfo != nil && accName == tenantInfo.GetTenant() {
		return nil, moerr.NewInternalError(ctx, "can not subscribe to self")
	}

	newCtx = context.WithValue(ctx, defines.TenantIDKey{}, catalog.System_Account)

	//get pubAccountId from publication info
	sql, err = getSqlForAccountIdAndStatus(newCtx, accName, true)
	if err != nil {
		return nil, err
	}
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return nil, err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(newCtx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(newCtx, bh)
	if err != nil {
		return nil, err
	}

	if !execResultArrayHasData(erArray) {
		return nil, moerr.NewInternalError(newCtx, "there is no publication account %s", accName)
	}
	accId, err = erArray[0].GetInt64(newCtx, 0, 0)
	if err != nil {
		return nil, err
	}

	accStatus, err = erArray[0].GetString(newCtx, 0, 1)
	if err != nil {
		return nil, err
	}

	if accStatus == tree.AccountStatusSuspend.String() {
		return nil, moerr.NewInternalError(newCtx, "the account %s is suspended", accName)
	}

	//check the publication is already exist or not

	newCtx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(accId))
	sql, err = getSqlForPubInfoForSub(newCtx, pubName, true)
	if err != nil {
		return nil, err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(newCtx, sql)
	if err != nil {
		return nil, err
	}
	if erArray, err = getResultSet(newCtx, bh); err != nil {
		return nil, err
	}
	if !execResultArrayHasData(erArray) {
		return nil, moerr.NewInternalError(newCtx, "there is no publication %s", pubName)
	}

	databaseName, err = erArray[0].GetString(newCtx, 0, 0)

	if err != nil {
		return nil, err
	}

	accountList, err = erArray[0].GetString(newCtx, 0, 1)
	if err != nil {
		return nil, err
	}

	if tenantInfo == nil {
		if ctx.Value(defines.TenantIDKey{}) != nil {
			value := ctx.Value(defines.TenantIDKey{})
			if tenantId, ok := value.(uint32); ok {
				sql = getSqlForGetAccountName(tenantId)
				bh.ClearExecResultSet()
				newCtx = context.WithValue(ctx, defines.TenantIDKey{}, catalog.System_Account)
				err = bh.Exec(newCtx, sql)
				if err != nil {
					return nil, err
				}
				if erArray, err = getResultSet(newCtx, bh); err != nil {
					return nil, err
				}
				if !execResultArrayHasData(erArray) {
					return nil, moerr.NewInternalError(newCtx, "there is no account, account id %d ", tenantId)
				}

				tenantName, err = erArray[0].GetString(newCtx, 0, 0)
				if err != nil {
					return nil, err
				}
				if !isSubscriptionValid(accountList, tenantName) {
					return nil, moerr.NewInternalError(newCtx, "the account %s is not allowed to subscribe the publication %s", tenantName, pubName)
				}
			}
		} else {
			return nil, moerr.NewInternalError(newCtx, "the subscribe %s is not valid", pubName)
		}
	} else if !isSubscriptionValid(accountList, tenantInfo.GetTenant()) {
		logErrorf(ses.GetDebugString(),
			"subName %s , accName %s, pubName %s, databaseName %s accountList %s account %s",
			subName, accName, pubName,
			databaseName, accountList,
			tenantInfo.GetTenant())
		return nil, moerr.NewInternalError(newCtx, "the account %s is not allowed to subscribe the publication %s", tenantInfo.GetTenant(), pubName)
	}

	subs = &plan.SubscriptionMeta{
		Name:        pubName,
		AccountId:   int32(accId),
		DbName:      databaseName,
		AccountName: accName,
		SubName:     subName,
	}

	return subs, err
}

func checkSubscriptionValid(ctx context.Context, ses *Session, createSql string) (*plan.SubscriptionMeta, error) {
	var (
		err                       error
		lowerAny                  any
		lowerInt64                int64
		accName, pubName, subName string
		ast                       []tree.Statement
	)
	lowerAny, err = ses.GetGlobalVar("lower_case_table_names")
	if err != nil {
		return nil, err
	}
	lowerInt64 = lowerAny.(int64)
	ast, err = mysql.Parse(ctx, createSql, lowerInt64)
	if err != nil {
		return nil, err
	}

	accName = string(ast[0].(*tree.CreateDatabase).SubscriptionOption.From)
	pubName = string(ast[0].(*tree.CreateDatabase).SubscriptionOption.Publication)
	subName = string(ast[0].(*tree.CreateDatabase).Name)

	return checkSubscriptionValidCommon(ctx, ses, subName, accName, pubName)
}

func isDbPublishing(ctx context.Context, dbName string, ses *Session) (ok bool, err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()
	var (
		sql     string
		erArray []ExecResult
		count   int64
	)

	sql, err = getSqlForDbPubCount(ctx, dbName)
	if err != nil {
		return false, err
	}
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return false, err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return false, err
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}
	if !execResultArrayHasData(erArray) {
		return false, moerr.NewInternalError(ctx, "there is no publication for database %s", dbName)
	}
	count, err = erArray[0].GetInt64(ctx, 0, 0)
	if err != nil {
		return false, err
	}

	return count > 0, err
}

func doCreatePublication(ctx context.Context, ses *Session, cp *tree.CreatePublication) (err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()
	const allTable = true
	var (
		sql         string
		erArray     []ExecResult
		datId       uint64
		datType     string
		tableList   string
		accountList string
		tenantInfo  *TenantInfo
	)

	tenantInfo = ses.GetTenantInfo()

	if !tenantInfo.IsAdminRole() {
		return moerr.NewInternalError(ctx, "only admin can create publication")
	}

	if cp.AccountsSet == nil || cp.AccountsSet.All {
		accountList = "all"
	} else {
		accts := make([]string, 0, len(cp.AccountsSet.SetAccounts))
		for _, acct := range cp.AccountsSet.SetAccounts {
			accName := string(acct)
			if accountNameIsInvalid(accName) {
				return moerr.NewInternalError(ctx, "invalid account name '%s'", accName)
			}
			accts = append(accts, accName)
		}
		sort.Strings(accts)
		accountList = strings.Join(accts, ",")
	}

	pubDb := string(cp.Database)

	if _, ok := sysDatabases[pubDb]; ok {
		return moerr.NewInternalError(ctx, "invalid database name '%s', not support publishing system database", pubDb)
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}
	bh.ClearExecResultSet()

	sql, err = getSqlForGetDbIdAndType(ctx, pubDb, true, uint64(tenantInfo.TenantID))
	if err != nil {
		return err
	}
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}
	if !execResultArrayHasData(erArray) {
		return moerr.NewInternalError(ctx, "database '%s' does not exist", cp.Database)
	}
	datId, err = erArray[0].GetUint64(ctx, 0, 0)
	if err != nil {
		return err
	}
	datType, err = erArray[0].GetString(ctx, 0, 1)
	if err != nil {
		return err
	}
	if datType != "" { //TODO: check the dat_type
		return moerr.NewInternalError(ctx, "database '%s' is not a user database", cp.Database)
	}
	bh.ClearExecResultSet()
	sql, err = getSqlForInsertIntoMoPubs(ctx, string(cp.Name), pubDb, datId, allTable, tableList, accountList, tenantInfo.GetDefaultRoleID(), tenantInfo.GetUserID(), cp.Comment, true)
	if err != nil {
		return err
	}
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}
	return err
}

func doAlterPublication(ctx context.Context, ses *Session, ap *tree.AlterPublication) (err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()
	var (
		allAccount     bool
		accountList    string
		accountListSep []string
		comment        string
		sql            string
		erArray        []ExecResult
		tenantInfo     *TenantInfo
	)

	tenantInfo = ses.GetTenantInfo()

	if !tenantInfo.IsAdminRole() {
		return moerr.NewInternalError(ctx, "only admin can alter publication")
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}
	bh.ClearExecResultSet()
	sql, err = getSqlForGetPubInfo(ctx, string(ap.Name), true)
	if err != nil {
		return err
	}
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}
	if !execResultArrayHasData(erArray) {
		return moerr.NewInternalError(ctx, "publication '%s' does not exist", ap.Name)
	}

	accountList, err = erArray[0].GetString(ctx, 0, 0)
	if err != nil {
		return err
	}
	allAccount = accountList == "all"

	comment, err = erArray[0].GetString(ctx, 0, 1)
	if err != nil {
		return err
	}

	if ap.AccountsSet != nil {
		switch {
		case ap.AccountsSet.All:
			accountList = "all"
		case len(ap.AccountsSet.SetAccounts) > 0:
			/* do not check accountName if exists here */
			accts := make([]string, 0, len(ap.AccountsSet.SetAccounts))
			for _, acct := range ap.AccountsSet.SetAccounts {
				s := string(acct)
				if accountNameIsInvalid(s) {
					return moerr.NewInternalError(ctx, "invalid account name '%s'", s)
				}
				accts = append(accts, s)
			}
			sort.Strings(accts)
			accountList = strings.Join(accts, ",")
		case len(ap.AccountsSet.DropAccounts) > 0:
			if allAccount {
				return moerr.NewInternalError(ctx, "cannot drop accounts from all account option")
			}
			accountListSep = strings.Split(accountList, ",")
			for _, acct := range ap.AccountsSet.DropAccounts {
				if accountNameIsInvalid(string(acct)) {
					return moerr.NewInternalError(ctx, "invalid account name '%s'", acct)
				}
				idx := sort.SearchStrings(accountListSep, string(acct))
				if idx < len(accountListSep) && accountListSep[idx] == string(acct) {
					accountListSep = append(accountListSep[:idx], accountListSep[idx+1:]...)
				}
			}
			accountList = strings.Join(accountListSep, ",")
		case len(ap.AccountsSet.AddAccounts) > 0:
			if allAccount {
				return moerr.NewInternalError(ctx, "cannot add account from all account option")
			}
			accountListSep = strings.Split(accountList, ",")
			for _, acct := range ap.AccountsSet.AddAccounts {
				if accountNameIsInvalid(string(acct)) {
					return moerr.NewInternalError(ctx, "invalid account name '%s'", acct)
				}
				idx := sort.SearchStrings(accountListSep, string(acct))
				if idx == len(accountListSep) || accountListSep[idx] != string(acct) {
					accountListSep = append(accountListSep[:idx], append([]string{string(acct)}, accountListSep[idx:]...)...)
				}
			}
			accountList = strings.Join(accountListSep, ",")
		}
	}
	if ap.Comment != "" {
		comment = ap.Comment
	}
	sql, err = getSqlForUpdatePubInfo(ctx, string(ap.Name), accountList, comment, false)
	if err != nil {
		return err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}
	return err
}

func doDropPublication(ctx context.Context, ses *Session, dp *tree.DropPublication) (err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()
	bh.ClearExecResultSet()
	var (
		sql        string
		erArray    []ExecResult
		tenantInfo *TenantInfo
	)

	tenantInfo = ses.GetTenantInfo()

	if !tenantInfo.IsAdminRole() {
		return moerr.NewInternalError(ctx, "only admin can drop publication")
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}
	sql, err = getSqlForGetPubInfo(ctx, string(dp.Name), true)
	if err != nil {
		return err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}
	if !execResultArrayHasData(erArray) {
		return moerr.NewInternalError(ctx, "publication '%s' does not exist", dp.Name)
	}

	sql, err = getSqlForDropPubInfo(ctx, string(dp.Name), false)
	if err != nil {
		return err
	}

	err = bh.Exec(ctx, sql)
	if err != nil {
		return err
	}

	return err
}

// doDropAccount accomplishes the DropAccount statement
func doDropAccount(ctx context.Context, ses *Session, da *tree.DropAccount) (err error) {
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//set backgroundHandler's default schema
	if handler, ok := bh.(*BackgroundHandler); ok {
		handler.ses.Session.txnCompileCtx.dbName = catalog.MO_CATALOG
	}

	var sql, db, table string
	var erArray []ExecResult
	var databases map[string]int8
	var dbSql, prefix string
	var sqlsForDropDatabases = make([]string, 0, 5)

	var deleteCtx context.Context
	var accountId int64
	var version uint64
	var hasAccount = true
	clusterTables := make(map[string]int)

	da.Name, err = normalizeName(ctx, da.Name)
	if err != nil {
		return err
	}

	if isSysTenant(da.Name) {
		return moerr.NewInternalError(ctx, "can not delete the account %s", da.Name)
	}

	dropAccountFunc := func() error {
		err = bh.Exec(ctx, "begin;")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return err
		}

		//check the account exists or not
		sql, err = getSqlForCheckTenant(ctx, da.Name)
		if err != nil {
			return err
		}
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			accountId, err = erArray[0].GetInt64(ctx, 0, 0)
			if err != nil {
				return err
			}
			version, err = erArray[0].GetUint64(ctx, 0, 3)
			if err != nil {
				return err
			}
		} else {
			//no such account
			if !da.IfExists { //when the "IF EXISTS" is set, just skip it.
				return moerr.NewInternalError(ctx, "there is no account %s", da.Name)
			}
			hasAccount = false
		}

		if !hasAccount {
			return err
		}

		//drop tables of the tenant
		//NOTE!!!: single DDL drop statement per single transaction
		//SWITCH TO THE CONTEXT of the deleted context
		deleteCtx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(accountId))

		//step 2 : drop table mo_user
		//step 3 : drop table mo_role
		//step 4 : drop table mo_user_grant
		//step 5 : drop table mo_role_grant
		//step 6 : drop table mo_role_privs
		//step 7 : drop table mo_user_defined_function
		//step 8 : drop table mo_mysql_compatibility_mode
		//step 9 : drop table %!%mo_increment_columns
		for _, sql = range getSqlForDropAccount() {
			err = bh.Exec(deleteCtx, sql)
			if err != nil {
				return err
			}
		}

		// delete all publications

		err = bh.Exec(deleteCtx, deleteMoPubsSql)

		if err != nil {
			return err
		}

		//drop databases created by user
		databases = make(map[string]int8)
		dbSql = "show databases;"
		bh.ClearExecResultSet()
		err = bh.Exec(deleteCtx, dbSql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			db, err = erArray[0].GetString(ctx, i, 0)
			if err != nil {
				return err
			}
			databases[db] = 0
		}

		prefix = "drop database if exists "

		for db = range databases {
			if db == "mo_catalog" {
				continue
			}
			bb := &bytes.Buffer{}
			bb.WriteString(prefix)
			//handle the database annotated by '`'
			if db != strings.ToLower(db) {
				bb.WriteString("`")
				bb.WriteString(db)
				bb.WriteString("`")
			} else {
				bb.WriteString(db)
			}
			bb.WriteString(";")
			sqlsForDropDatabases = append(sqlsForDropDatabases, bb.String())
		}

		for _, sql = range sqlsForDropDatabases {
			err = bh.Exec(deleteCtx, sql)
			if err != nil {
				return err
			}
		}

		//  drop table mo_pubs
		err = bh.Exec(deleteCtx, dropMoPubsSql)
		if err != nil {
			return err
		}

		// drop autoIcr table
		err = bh.Exec(deleteCtx, dropAutoIcrColSql)
		if err != nil {
			return err
		}

		//step 11: drop mo_catalog.mo_indexes under general tenant
		err = bh.Exec(deleteCtx, dropMoIndexes)
		if err != nil {
			return err
		}

		//step 1 : delete the account in the mo_account of the sys account
		sql, err = getSqlForDeleteAccountFromMoAccount(ctx, da.Name)
		if err != nil {
			return err
		}
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		//step 2: get all cluster table in the mo_catalog

		sql = "show tables from mo_catalog;"
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			table, err = erArray[0].GetString(ctx, i, 0)
			if err != nil {
				return err
			}
			if isClusterTable("mo_catalog", table) {
				clusterTables[table] = 0
			}
		}

		//step3 : delete all data of the account in the cluster table
		for clusterTable := range clusterTables {
			sql = fmt.Sprintf("delete from mo_catalog.`%s` where account_id = %d;", clusterTable, accountId)
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
		}
		return err
	}

	err = dropAccountFunc()
	if err != nil {
		return err
	}

	//if drop the account, add the account to kill queue
	ses.getRoutineManager().accountRoutine.enKillQueue(accountId, version)

	return err
}

// doDropUser accomplishes the DropUser statement
func doDropUser(ctx context.Context, ses *Session, du *tree.DropUser) (err error) {
	var vr *verifiedRole
	var sql string
	var sqls []string
	var erArray []ExecResult
	account := ses.GetTenantInfo()
	err = normalizeNamesOfUsers(ctx, du.Users)
	if err != nil {
		return err
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	//step1: check users exists or not.
	//handle "IF EXISTS"
	for _, user := range du.Users {
		sql, err = getSqlForPasswordOfUser(ctx, user.Username)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, user.Username, roleType)
		if err != nil {
			return err
		}

		if vr == nil {
			if !du.IfExists { //when the "IF EXISTS" is set, just skip it.
				return moerr.NewInternalError(ctx, "there is no user %s", user.Username)
			}
		}

		if vr == nil {
			continue
		}

		//if the user is admin user with the role moadmin or accountadmin,
		//the user can not be deleted.
		if account.IsSysTenant() {
			sql, err = getSqlForCheckUserHasRole(ctx, user.Username, moAdminRoleID)
		} else {
			sql, err = getSqlForCheckUserHasRole(ctx, user.Username, accountAdminRoleID)
		}
		if err != nil {
			return err
		}

		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			return moerr.NewInternalError(ctx, "can not delete the user %s", user.Username)
		}

		//step2 : delete mo_user
		//step3 : delete mo_user_grant
		sqls = getSqlForDeleteUser(vr.id)
		for _, sqlx := range sqls {
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sqlx)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// doDropRole accomplishes the DropRole statement
func doDropRole(ctx context.Context, ses *Session, dr *tree.DropRole) (err error) {
	var vr *verifiedRole
	var sql string
	account := ses.GetTenantInfo()
	err = normalizeNamesOfRoles(ctx, dr.Roles)
	if err != nil {
		return err
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	//step1: check roles exists or not.
	//handle "IF EXISTS"
	for _, role := range dr.Roles {
		sql, err = getSqlForRoleIdOfRole(ctx, role.UserName)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, role.UserName, roleType)
		if err != nil {
			return err
		}

		if vr == nil {
			if !dr.IfExists { //when the "IF EXISTS" is set, just skip it.
				return moerr.NewInternalError(ctx, "there is no role %s", role.UserName)
			}
		}

		//step2 : delete mo_role
		//step3 : delete mo_user_grant
		//step4 : delete mo_role_grant
		//step5 : delete mo_role_privs
		if vr == nil {
			continue
		}

		//NOTE: if the role is the admin role (moadmin,accountadmin) or public,
		//the role can not be deleted.
		if account.IsNameOfAdminRoles(vr.name) || isPublicRole(vr.name) {
			return moerr.NewInternalError(ctx, "can not delete the role %s", vr.name)
		}

		sqls := getSqlForDeleteRole(vr.id)
		for _, sqlx := range sqls {
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sqlx)
			if err != nil {
				return err
			}
		}
	}

	return err
}

func doDropFunction(ctx context.Context, ses *Session, df *tree.DropFunction) (err error) {
	var sql string
	var argstr string
	var checkDatabase string
	var dbName string
	var funcId int64
	var fmtctx *tree.FmtCtx
	var erArray []ExecResult

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	// a database must be selected or specified as qualifier when create a function
	if df.Name.HasNoNameQualifier() {
		if ses.DatabaseNameIsEmpty() {
			return moerr.NewNoDBNoCtx()
		}
		dbName = ses.GetDatabaseName()
	} else {
		dbName = string(df.Name.Name.SchemaName)
	}

	fmtctx = tree.NewFmtCtx(dialect.MYSQL, tree.WithQuoteString(true))

	// validate database name and signature (name + args)
	bh.ClearExecResultSet()
	checkDatabase = fmt.Sprintf(checkUdfArgs, string(df.Name.Name.ObjectName), dbName)
	err = bh.Exec(ctx, checkDatabase)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}

	if execResultArrayHasData(erArray) {
		// function with provided name and db exists, now check arguments
		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			argstr, err = erArray[0].GetString(ctx, i, 0)
			if err != nil {
				return err
			}
			funcId, err = erArray[0].GetInt64(ctx, i, 1)
			if err != nil {
				return err
			}
			argMap := make(map[string]string)
			json.Unmarshal([]byte(argstr), &argMap)
			argCount := 0
			if len(argMap) == len(df.Args) {
				for _, v := range argMap {
					if v != (df.Args[argCount].GetType(fmtctx)) {
						return moerr.NewInvalidInput(ctx, "invalid parameter")
					}
					argCount++
					fmtctx.Reset()
				}
				handleArgMatch := func() error {
					//put it into the single transaction
					err = bh.Exec(ctx, "begin;")
					defer func() {
						err = finishTxn(ctx, bh, err)
					}()
					if err != nil {
						return err
					}

					sql = fmt.Sprintf(deleteUserDefinedFunctionFormat, funcId)

					err = bh.Exec(ctx, sql)
					if err != nil {
						return err
					}
					return err
				}
				return handleArgMatch()
			}
		}
		return err
	} else {
		// no such function
		return moerr.NewNoUDFNoCtx(string(df.Name.Name.ObjectName))
	}
}

func doDropProcedure(ctx context.Context, ses *Session, dp *tree.DropProcedure) (err error) {
	var sql string
	var checkDatabase string
	var dbName string
	var procId int64
	var erArray []ExecResult

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	if dp.Name.HasNoNameQualifier() {
		if ses.DatabaseNameIsEmpty() {
			return moerr.NewNoDBNoCtx()
		}
		dbName = ses.GetDatabaseName()
	} else {
		dbName = string(dp.Name.Name.SchemaName)
	}

	// validate database name and signature (name + args)
	bh.ClearExecResultSet()
	checkDatabase = fmt.Sprintf(checkStoredProcedureArgs, string(dp.Name.Name.ObjectName), dbName)
	err = bh.Exec(ctx, checkDatabase)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}

	if execResultArrayHasData(erArray) {
		// function with provided name and db exists, for now we don't support overloading for stored procedure, so go to handle deletion.
		procId, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return err
		}
		handleArgMatch := func() error {
			//put it into the single transaction
			err = bh.Exec(ctx, "begin;")
			defer func() {
				err = finishTxn(ctx, bh, err)
			}()
			if err != nil {
				return err
			}

			sql = fmt.Sprintf(deleteStoredProcedureFormat, procId)

			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
			return err
		}
		return handleArgMatch()
	} else {
		// no such procedure
		if dp.IfExists {
			return nil
		}
		return moerr.NewNoUDFNoCtx(string(dp.Name.Name.ObjectName))
	}
}

// doRevokePrivilege accomplishes the RevokePrivilege statement
func doRevokePrivilege(ctx context.Context, ses *Session, rp *tree.RevokePrivilege) (err error) {
	var vr *verifiedRole
	var objType objectType
	var privLevel privilegeLevelType
	var objId int64
	var privType PrivilegeType
	var sql string
	err = normalizeNamesOfRoles(ctx, rp.Roles)
	if err != nil {
		return err
	}

	account := ses.GetTenantInfo()
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	verifiedRoles := make([]*verifiedRole, len(rp.Roles))
	checkedPrivilegeTypes := make([]PrivilegeType, len(rp.Privileges))

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	//handle "IF EXISTS"
	//step 1: check roles. exists or not.
	for i, user := range rp.Roles {
		//check Revoke privilege on xxx yyy from moadmin(accountadmin)
		if account.IsNameOfAdminRoles(user.UserName) {
			return moerr.NewInternalError(ctx, "the privilege can not be revoked from the role %s", user.UserName)
		}
		sql, err = getSqlForRoleIdOfRole(ctx, user.UserName)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, user.UserName, roleType)
		if err != nil {
			return err
		}
		verifiedRoles[i] = vr
		if vr == nil {
			if !rp.IfExists { //when the "IF EXISTS" is set, just skip it.
				return moerr.NewInternalError(ctx, "there is no role %s", user.UserName)
			}
		}
	}

	//get the object type
	objType, err = convertAstObjectTypeToObjectType(ctx, rp.ObjType)
	if err != nil {
		return err
	}

	//check the privilege and the object type
	for i, priv := range rp.Privileges {
		privType, err = convertAstPrivilegeTypeToPrivilegeType(ctx, priv.Type, rp.ObjType)
		if err != nil {
			return err
		}
		//check the match between the privilegeScope and the objectType
		err = matchPrivilegeTypeWithObjectType(ctx, privType, objType)
		if err != nil {
			return err
		}
		checkedPrivilegeTypes[i] = privType
	}

	//step 2: decide the object type , the object id and the privilege_level
	privLevel, objId, err = checkPrivilegeObjectTypeAndPrivilegeLevel(ctx, ses, bh, rp.ObjType, *rp.Level)
	if err != nil {
		return err
	}

	//step 3: delete the granted privilege
	for _, privType = range checkedPrivilegeTypes {
		for _, role := range verifiedRoles {
			if role == nil {
				continue
			}
			if privType == PrivilegeTypeConnect && isPublicRole(role.name) {
				return moerr.NewInternalError(ctx, "the privilege %s can not be revoked from the role %s", privType, role.name)
			}
			sql = getSqlForDeleteRolePrivs(role.id, objType.String(), objId, int64(privType), privLevel.String())
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// getDatabaseOrTableId gets the id of the database or the table
func getDatabaseOrTableId(ctx context.Context, bh BackgroundExec, isDb bool, dbName, tableName string) (int64, error) {
	var err error
	var sql string
	var erArray []ExecResult
	var id int64
	if isDb {
		sql, err = getSqlForCheckDatabase(ctx, dbName)
	} else {
		sql, err = getSqlForCheckDatabaseTable(ctx, dbName, tableName)
	}
	if err != nil {
		return 0, err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return 0, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return 0, err
	}

	if execResultArrayHasData(erArray) {
		id, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return 0, err
		}
		return id, nil
	}
	if isDb {
		return 0, moerr.NewInternalError(ctx, `there is no database "%s"`, dbName)
	} else {
		//TODO: check the database exists or not first
		return 0, moerr.NewInternalError(ctx, `there is no table "%s" in database "%s"`, tableName, dbName)
	}
}

// convertAstObjectTypeToObjectType gets the object type from the ast
func convertAstObjectTypeToObjectType(ctx context.Context, ot tree.ObjectType) (objectType, error) {
	var objType objectType
	switch ot {
	case tree.OBJECT_TYPE_TABLE:
		objType = objectTypeTable
	case tree.OBJECT_TYPE_DATABASE:
		objType = objectTypeDatabase
	case tree.OBJECT_TYPE_ACCOUNT:
		objType = objectTypeAccount
	default:
		return 0, moerr.NewInternalError(ctx, `the object type "%s" is unsupported`, ot.String())
	}
	return objType, nil
}

// checkPrivilegeObjectTypeAndPrivilegeLevel checks the relationship among the privilege type, the object type and the privilege level.
// it returns the converted object type, the privilege level and the object id.
func checkPrivilegeObjectTypeAndPrivilegeLevel(ctx context.Context, ses *Session, bh BackgroundExec,
	ot tree.ObjectType, pl tree.PrivilegeLevel) (privilegeLevelType, int64, error) {
	var privLevel privilegeLevelType
	var objId int64
	var err error
	var dbName string

	switch ot {
	case tree.OBJECT_TYPE_TABLE:
		switch pl.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			privLevel = privilegeLevelStar
			objId, err = getDatabaseOrTableId(ctx, bh, true, ses.GetDatabaseName(), "")
			if err != nil {
				return 0, 0, err
			}
		case tree.PRIVILEGE_LEVEL_TYPE_STAR_STAR:
			privLevel = privilegeLevelStarStar
			objId = objectIDAll
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE_STAR:
			privLevel = privilegeLevelDatabaseStar
			objId, err = getDatabaseOrTableId(ctx, bh, true, pl.DbName, "")
			if err != nil {
				return 0, 0, err
			}
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE_TABLE:
			privLevel = privilegeLevelDatabaseTable
			objId, err = getDatabaseOrTableId(ctx, bh, false, pl.DbName, pl.TabName)
			if err != nil {
				return 0, 0, err
			}
		case tree.PRIVILEGE_LEVEL_TYPE_TABLE:
			privLevel = privilegeLevelTable
			objId, err = getDatabaseOrTableId(ctx, bh, false, ses.GetDatabaseName(), pl.TabName)
			if err != nil {
				return 0, 0, err
			}
		default:
			err = moerr.NewInternalError(ctx, `in the object type "%s" the privilege level "%s" is unsupported`, ot.String(), pl.String())
			return 0, 0, err
		}
	case tree.OBJECT_TYPE_DATABASE:
		switch pl.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			privLevel = privilegeLevelStar
			objId = objectIDAll
		case tree.PRIVILEGE_LEVEL_TYPE_STAR_STAR:
			privLevel = privilegeLevelStarStar
			objId = objectIDAll
		case tree.PRIVILEGE_LEVEL_TYPE_TABLE:
			//in the syntax, we can not distinguish the table name from the database name.
			privLevel = privilegeLevelDatabase
			dbName = pl.TabName
			objId, err = getDatabaseOrTableId(ctx, bh, true, dbName, "")
			if err != nil {
				return 0, 0, err
			}
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE:
			privLevel = privilegeLevelDatabase
			dbName = pl.DbName
			objId, err = getDatabaseOrTableId(ctx, bh, true, dbName, "")
			if err != nil {
				return 0, 0, err
			}
		default:
			err = moerr.NewInternalError(ctx, `in the object type "%s" the privilege level "%s" is unsupported`, ot.String(), pl.String())
			return 0, 0, err
		}
	case tree.OBJECT_TYPE_ACCOUNT:
		switch pl.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			privLevel = privilegeLevelStar
			objId = objectIDAll
		default:
			err = moerr.NewInternalError(ctx, `in the object type "%s" the privilege level "%s" is unsupported`, ot.String(), pl.String())
			return 0, 0, err
		}
	default:
		err = moerr.NewInternalError(ctx, `the object type "%s" is unsupported`, ot.String())
		return 0, 0, err
	}

	return privLevel, objId, err
}

// matchPrivilegeTypeWithObjectType matches the privilege type with the object type
func matchPrivilegeTypeWithObjectType(ctx context.Context, privType PrivilegeType, objType objectType) error {
	var err error
	switch privType.Scope() {
	case PrivilegeScopeSys, PrivilegeScopeAccount, PrivilegeScopeUser, PrivilegeScopeRole:
		if objType != objectTypeAccount {
			err = moerr.NewInternalError(ctx, `the privilege "%s" can only be granted to the object type "account"`, privType)
		}
	case PrivilegeScopeDatabase:
		if objType != objectTypeDatabase {
			err = moerr.NewInternalError(ctx, `the privilege "%s" can only be granted to the object type "database"`, privType)
		}
	case PrivilegeScopeTable:
		if objType != objectTypeTable {
			err = moerr.NewInternalError(ctx, `the privilege "%s" can only be granted to the object type "table"`, privType)
		}
	case PrivilegeScopeRoutine:
		if objType != objectTypeFunction {
			err = moerr.NewInternalError(ctx, `the privilege "%s" can only be granted to the object type "function"`, privType)
		}
	}
	return err
}

// doGrantPrivilege accomplishes the GrantPrivilege statement
func doGrantPrivilege(ctx context.Context, ses *Session, gp *tree.GrantPrivilege) (err error) {
	var erArray []ExecResult
	var roleId int64
	var privType PrivilegeType
	var objType objectType
	var privLevel privilegeLevelType
	var objId int64
	var sql string
	var userId uint32

	err = normalizeNamesOfRoles(ctx, gp.Roles)
	if err != nil {
		return err
	}

	account := ses.GetTenantInfo()
	if account == nil {
		ctxUserId := ctx.Value(defines.UserIDKey{})
		if id, ok := ctxUserId.(uint32); ok {
			userId = id
		}
	} else {
		userId = account.GetUserID()
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//Get primary keys
	//step 1: get role_id
	verifiedRoles := make([]*verifiedRole, len(gp.Roles))
	checkedPrivilegeTypes := make([]PrivilegeType, len(gp.Privileges))

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	for i, role := range gp.Roles {
		//check Grant privilege on xxx yyy to moadmin(accountadmin)
		if account != nil && account.IsNameOfAdminRoles(role.UserName) {
			return moerr.NewInternalError(ctx, "the privilege can not be granted to the role %s", role.UserName)
		}
		sql, err = getSqlForRoleIdOfRole(ctx, role.UserName)
		if err != nil {
			return err
		}
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			for j := uint64(0); j < erArray[0].GetRowCount(); j++ {
				roleId, err = erArray[0].GetInt64(ctx, j, 0)
				if err != nil {
					return err
				}
			}
		} else {
			return moerr.NewInternalError(ctx, "there is no role %s", role.UserName)
		}
		verifiedRoles[i] = &verifiedRole{
			typ:  roleType,
			name: role.UserName,
			id:   roleId,
		}
	}

	//get the object type
	objType, err = convertAstObjectTypeToObjectType(ctx, gp.ObjType)
	if err != nil {
		return err
	}

	//check the privilege and the object type
	for i, priv := range gp.Privileges {
		privType, err = convertAstPrivilegeTypeToPrivilegeType(ctx, priv.Type, gp.ObjType)
		if err != nil {
			return err
		}
		if isBannedPrivilege(privType) {
			return moerr.NewInternalError(ctx, "the privilege %s can not be granted", privType)
		}
		//check the match between the privilegeScope and the objectType
		err = matchPrivilegeTypeWithObjectType(ctx, privType, objType)
		if err != nil {
			return err
		}
		checkedPrivilegeTypes[i] = privType
	}

	//step 2: get obj_type, privilege_level
	//step 3: get obj_id
	privLevel, objId, err = checkPrivilegeObjectTypeAndPrivilegeLevel(ctx, ses, bh, gp.ObjType, *gp.Level)
	if err != nil {
		return err
	}

	//step 4: get privilege_id
	//step 5: check exists
	//step 6: update or insert

	for _, privType = range checkedPrivilegeTypes {
		for _, role := range verifiedRoles {
			sql = getSqlForCheckRoleHasPrivilege(role.id, objType, objId, int64(privType))
			//check exists
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}

			//choice 1 : update the record
			//choice 2 : inset new record
			choice := 1
			if execResultArrayHasData(erArray) {
				for j := uint64(0); j < erArray[0].GetRowCount(); j++ {
					_, err = erArray[0].GetInt64(ctx, j, 0)
					if err != nil {
						return err
					}
				}
			} else {
				choice = 2
			}

			if choice == 1 { //update the record
				sql = getSqlForUpdateRolePrivs(int64(userId),
					types.CurrentTimestamp().String2(time.UTC, 0),
					gp.GrantOption, role.id, objType, objId, int64(privType))
			} else if choice == 2 { //insert new record
				sql = getSqlForInsertRolePrivs(role.id, role.name, objType.String(), objId,
					int64(privType), privType.String(), privLevel.String(), int64(userId),
					types.CurrentTimestamp().String2(time.UTC, 0), gp.GrantOption)
			}

			//insert or update
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
		}
	}

	return err
}

// doRevokeRole accomplishes the RevokeRole statement
func doRevokeRole(ctx context.Context, ses *Session, rr *tree.RevokeRole) (err error) {
	var sql string
	err = normalizeNamesOfRoles(ctx, rr.Roles)
	if err != nil {
		return err
	}
	err = normalizeNamesOfUsers(ctx, rr.Users)
	if err != nil {
		return err
	}

	account := ses.GetTenantInfo()
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//step1 : check Roles exists or not
	var vr *verifiedRole

	verifiedFromRoles := make([]*verifiedRole, len(rr.Roles))
	verifiedToRoles := make([]*verifiedRole, len(rr.Users))

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	//handle "IF EXISTS"
	//step1 : check Users are real Users or Roles,  exists or not
	for i, user := range rr.Users {
		sql, err = getSqlForRoleIdOfRole(ctx, user.Username)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, user.Username, roleType)
		if err != nil {
			return err
		}
		if vr != nil {
			verifiedToRoles[i] = vr
		} else {
			//check user
			sql, err = getSqlForPasswordOfUser(ctx, user.Username)
			if err != nil {
				return err
			}
			vr, err = verifyRoleFunc(ctx, bh, sql, user.Username, userType)
			if err != nil {
				return err
			}
			verifiedToRoles[i] = vr
			if vr == nil {
				if !rr.IfExists { //when the "IF EXISTS" is set, just skip the check
					return moerr.NewInternalError(ctx, "there is no role or user %s", user.Username)
				}
			}
		}
	}

	//handle "IF EXISTS"
	//step2 : check roles before the FROM clause
	for i, role := range rr.Roles {
		sql, err = getSqlForRoleIdOfRole(ctx, role.UserName)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, role.UserName, roleType)
		if err != nil {
			return err
		}
		if vr == nil {
			return moerr.NewInternalError(ctx, "there is no role %s", role.UserName)
		}
		verifiedFromRoles[i] = vr
	}

	//step3 : process Revoke role from role
	//step4 : process Revoke role from user
	for _, from := range verifiedFromRoles {
		for _, to := range verifiedToRoles {
			if to == nil { //Under "IF EXISTS"
				continue
			}
			if account.IsNameOfAdminRoles(from.name) {
				//check Revoke moadmin from root,dump,userX
				//check Revoke accountadmin from root,dump,userX
				//check Revoke moadmin(accountadmin) from roleX
				return moerr.NewInternalError(ctx, "the role %s can not be revoked", from.name)
			} else if isPublicRole(from.name) {
				//
				return moerr.NewInternalError(ctx, "the role %s can not be revoked", from.name)
			}

			if to.typ == roleType {
				//check Revoke roleX from moadmin(accountadmin)
				if account.IsNameOfAdminRoles(to.name) {
					return moerr.NewInternalError(ctx, "the role %s can not be revoked from the role %s", from.name, to.name)
				} else if isPublicRole(to.name) {
					//check Revoke roleX from public
					return moerr.NewInternalError(ctx, "the role %s can not be revoked from the role %s", from.name, to.name)
				}
			}

			if to.typ == roleType {
				//revoke from role
				//delete (granted_id,grantee_id) from the mo_role_grant
				sql = getSqlForDeleteRoleGrant(from.id, to.id)
			} else {
				//revoke from user
				//delete (roleId,userId) from the mo_user_grant
				sql = getSqlForDeleteUserGrant(from.id, to.id)
			}
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
		}
	}

	return err
}

// verifySpecialRolesInGrant verifies the special roles in the Grant statement
func verifySpecialRolesInGrant(ctx context.Context, account *TenantInfo, from, to *verifiedRole) error {
	if account.IsNameOfAdminRoles(from.name) {
		if to.typ == userType {
			//check Grant moadmin to root,dump
			//check Grant accountadmin to admin_name
			//check Grant moadmin to userX
			//check Grant accountadmin to userX
			if !to.userIsAdmin {
				return moerr.NewInternalError(ctx, "the role %s can not be granted to non administration user %s", from.name, to.name)
			}
		} else {
			//check Grant moadmin(accountadmin) to roleX
			if !account.IsNameOfAdminRoles(to.name) {
				return moerr.NewInternalError(ctx, "the role %s can not be granted to the other role %s", from.name, to.name)
			}
		}
	} else if isPublicRole(from.name) && to.typ == roleType {
		return moerr.NewInternalError(ctx, "the role %s can not be granted to the other role %s", from.name, to.name)
	}

	if to.typ == roleType {
		//check Grant roleX to moadmin(accountadmin)
		if account.IsNameOfAdminRoles(to.name) {
			return moerr.NewInternalError(ctx, "the role %s can not be granted to the role %s", from.name, to.name)
		} else if isPublicRole(to.name) {
			//check Grant roleX to public
			return moerr.NewInternalError(ctx, "the role %s can not be granted to the role %s", from.name, to.name)
		}
	}
	return nil
}

// doGrantRole accomplishes the GrantRole statement
func doGrantRole(ctx context.Context, ses *Session, gr *tree.GrantRole) (err error) {
	var erArray []ExecResult
	var withGrantOption int64
	var sql string
	err = normalizeNamesOfRoles(ctx, gr.Roles)
	if err != nil {
		return err
	}
	err = normalizeNamesOfUsers(ctx, gr.Users)
	if err != nil {
		return err
	}

	account := ses.GetTenantInfo()
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//step1 : check Roles exists or not
	var vr *verifiedRole
	var needLoadMoRoleGrant bool
	var grantedId, granteeId int64
	var useIsAdmin bool

	verifiedFromRoles := make([]*verifiedRole, len(gr.Roles))
	verifiedToRoles := make([]*verifiedRole, len(gr.Users))

	//load mo_role_grant into memory for
	checkLoopGraph := NewGraph()

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	for i, role := range gr.Roles {
		sql, err = getSqlForRoleIdOfRole(ctx, role.UserName)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, role.UserName, roleType)
		if err != nil {
			return err
		}
		if vr == nil {
			return moerr.NewInternalError(ctx, "there is no role %s", role.UserName)
		}
		verifiedFromRoles[i] = vr
	}

	//step2 : check Users are real Users or Roles,  exists or not
	for i, user := range gr.Users {
		sql, err = getSqlForRoleIdOfRole(ctx, user.Username)
		if err != nil {
			return err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, user.Username, roleType)
		if err != nil {
			return err
		}
		if vr != nil {
			verifiedToRoles[i] = vr
		} else {
			//check user exists or not
			sql, err = getSqlForPasswordOfUser(ctx, user.Username)
			if err != nil {
				return err
			}
			vr, err = verifyRoleFunc(ctx, bh, sql, user.Username, userType)
			if err != nil {
				return err
			}
			if vr == nil {
				return moerr.NewInternalError(ctx, "there is no role or user %s", user.Username)
			}
			verifiedToRoles[i] = vr

			//the user is the administrator or not
			useIsAdmin, err = userIsAdministrator(ctx, bh, vr.id, account)
			if err != nil {
				return err
			}
			verifiedToRoles[i].userIsAdmin = useIsAdmin
		}
	}

	//If there is at least one role in the verifiedToRoles,
	//it is necessary to load the mo_role_grant
	for _, role := range verifiedToRoles {
		if role.typ == roleType {
			needLoadMoRoleGrant = true
			break
		}
	}

	if needLoadMoRoleGrant {
		//load mo_role_grant
		sql = getSqlForGetAllStuffRoleGrantFormat()
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			for j := uint64(0); j < erArray[0].GetRowCount(); j++ {
				//column grantedId
				grantedId, err = erArray[0].GetInt64(ctx, j, 0)
				if err != nil {
					return err
				}

				//column granteeId
				granteeId, err = erArray[0].GetInt64(ctx, j, 1)
				if err != nil {
					return err
				}

				checkLoopGraph.addEdge(grantedId, granteeId)
			}
		}
	}

	//step3 : process Grant role to role
	//step4 : process Grant role to user

	for _, from := range verifiedFromRoles {
		for _, to := range verifiedToRoles {
			err = verifySpecialRolesInGrant(ctx, account, from, to)
			if err != nil {
				return err
			}

			if to.typ == roleType {
				if from.id == to.id { //direct loop
					return moerr.NewRoleGrantedToSelf(ctx, from.name, to.name)
				} else {
					//check the indirect loop
					edgeId := checkLoopGraph.addEdge(from.id, to.id)
					has := checkLoopGraph.hasLoop(from.id)
					if has {
						return moerr.NewRoleGrantedToSelf(ctx, from.name, to.name)
					}
					//restore the graph
					checkLoopGraph.removeEdge(edgeId)
				}

				//grant to role
				//get (granted_id,grantee_id,with_grant_option) from the mo_role_grant
				sql = getSqlForCheckRoleGrant(from.id, to.id)
			} else {
				//grant to user
				//get (roleId,userId,with_grant_option) from the mo_user_grant
				sql = getSqlForCheckUserGrant(from.id, to.id)
			}
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}

			//For Grant role to role
			//choice 1: (granted_id,grantee_id) exists and with_grant_option is same.
			//	Do nothing.
			//choice 2: (granted_id,grantee_id) exists and with_grant_option is different.
			//	Update.
			//choice 3: (granted_id,grantee_id) does not exist.
			// Insert.

			//For Grant role to user
			//choice 1: (roleId,userId) exists and with_grant_option is same.
			//	Do nothing.
			//choice 2: (roleId,userId) exists and with_grant_option is different.
			//	Update.
			//choice 3: (roleId,userId) does not exist.
			// Insert.
			choice := 1
			if execResultArrayHasData(erArray) {
				for j := uint64(0); j < erArray[0].GetRowCount(); j++ {
					withGrantOption, err = erArray[0].GetInt64(ctx, j, 2)
					if err != nil {
						return err
					}
					if (withGrantOption == 1) != gr.GrantOption {
						choice = 2
					}
				}
			} else {
				choice = 3
			}

			sql = ""
			if choice == 2 {
				//update grant time
				if to.typ == roleType {
					sql = getSqlForUpdateRoleGrant(from.id, to.id, int64(account.GetDefaultRoleID()), int64(account.GetUserID()), types.CurrentTimestamp().String2(time.UTC, 0), gr.GrantOption)
				} else {
					sql = getSqlForUpdateUserGrant(from.id, to.id, types.CurrentTimestamp().String2(time.UTC, 0), gr.GrantOption)
				}
			} else if choice == 3 {
				//insert new record
				if to.typ == roleType {
					sql = getSqlForInsertRoleGrant(from.id, to.id, int64(account.GetDefaultRoleID()), int64(account.GetUserID()), types.CurrentTimestamp().String2(time.UTC, 0), gr.GrantOption)
				} else {
					sql = getSqlForInsertUserGrant(from.id, to.id, types.CurrentTimestamp().String2(time.UTC, 0), gr.GrantOption)
				}
			}

			if choice != 1 {
				err = bh.Exec(ctx, sql)
				if err != nil {
					return err
				}
			}
		}
	}

	return err
}

// determinePrivilegeSetOfStatement decides the privileges that the statement needs before running it.
// That is the Set P for the privilege Set .
func determinePrivilegeSetOfStatement(stmt tree.Statement) *privilege {
	typs := make([]PrivilegeType, 0, 5)
	kind := privilegeKindGeneral
	special := specialTagNone
	objType := objectTypeAccount
	var extraEntries []privilegeEntry
	writeDatabaseAndTableDirectly := false
	var clusterTable bool
	var clusterTableOperation clusterTableOperationType
	dbName := ""
	switch st := stmt.(type) {
	case *tree.CreateAccount:
		typs = append(typs, PrivilegeTypeCreateAccount)
	case *tree.DropAccount:
		typs = append(typs, PrivilegeTypeDropAccount)
	case *tree.AlterAccount:
		typs = append(typs, PrivilegeTypeAlterAccount)
	case *tree.CreateUser:
		if st.Role == nil {
			typs = append(typs, PrivilegeTypeCreateUser, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
		} else {
			typs = append(typs, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
			me1 := &compoundEntry{
				items: []privilegeItem{
					{privilegeTyp: PrivilegeTypeCreateUser},
					{privilegeTyp: PrivilegeTypeManageGrants},
				},
			}
			me2 := &compoundEntry{
				items: []privilegeItem{
					{privilegeTyp: PrivilegeTypeCreateUser},
					{privilegeTyp: PrivilegeTypeCanGrantRoleToOthersInCreateUser, role: st.Role, users: st.Users},
				},
			}

			entry1 := privilegeEntry{
				privilegeEntryTyp: privilegeEntryTypeCompound,
				compound:          me1,
			}
			entry2 := privilegeEntry{
				privilegeEntryTyp: privilegeEntryTypeCompound,
				compound:          me2,
			}
			extraEntries = append(extraEntries, entry1, entry2)
		}
	case *tree.DropUser:
		typs = append(typs, PrivilegeTypeDropUser, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeUserOwnership*/)
	case *tree.AlterUser:
		typs = append(typs, PrivilegeTypeAlterUser, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeUserOwnership*/)
	case *tree.CreateRole:
		typs = append(typs, PrivilegeTypeCreateRole, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
	case *tree.DropRole:
		typs = append(typs, PrivilegeTypeDropRole, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeRoleOwnership*/)
	case *tree.Grant:
		if st.Typ == tree.GrantTypeRole {
			kind = privilegeKindInherit
			typs = append(typs, PrivilegeTypeManageGrants, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeRoleOwnership*/)
		} else if st.Typ == tree.GrantTypePrivilege {
			objType = objectTypeNone
			kind = privilegeKindSpecial
			special = specialTagAdmin | specialTagWithGrantOption | specialTagOwnerOfObject
		}
	case *tree.GrantRole:
		kind = privilegeKindInherit
		typs = append(typs, PrivilegeTypeManageGrants, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeRoleOwnership*/)
	case *tree.GrantPrivilege:
		objType = objectTypeNone
		kind = privilegeKindSpecial
		special = specialTagAdmin | specialTagWithGrantOption | specialTagOwnerOfObject
	case *tree.Revoke:
		if st.Typ == tree.RevokeTypeRole {
			typs = append(typs, PrivilegeTypeManageGrants, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeRoleOwnership*/)
		} else if st.Typ == tree.RevokeTypePrivilege {
			objType = objectTypeNone
			kind = privilegeKindSpecial
			special = specialTagAdmin
		}
	case *tree.RevokeRole:
		typs = append(typs, PrivilegeTypeManageGrants, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership, PrivilegeTypeRoleOwnership*/)
	case *tree.RevokePrivilege:
		objType = objectTypeNone
		kind = privilegeKindSpecial
		special = specialTagAdmin
	case *tree.CreateDatabase:
		typs = append(typs, PrivilegeTypeCreateDatabase, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
	case *tree.DropDatabase:
		typs = append(typs, PrivilegeTypeDropDatabase, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
		writeDatabaseAndTableDirectly = true
		dbName = string(st.Name)
	case *tree.ShowDatabases:
		typs = append(typs, PrivilegeTypeShowDatabases, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
	case *tree.ShowSequences:
		typs = append(typs, PrivilegeTypeAccountAll, PrivilegeTypeDatabaseOwnership)
	case *tree.Use:
		typs = append(typs, PrivilegeTypeConnect, PrivilegeTypeAccountAll /*, PrivilegeTypeAccountOwnership*/)
	case *tree.ShowTables, *tree.ShowCreateTable, *tree.ShowColumns, *tree.ShowCreateView, *tree.ShowCreateDatabase, *tree.ShowCreatePublications:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeShowTables, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
	case *tree.CreateTable:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateTable, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if st.IsClusterTable {
			clusterTable = true
			clusterTableOperation = clusterTableCreate
		}
		dbName = string(st.Table.SchemaName)
	case *tree.CreateView:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Name != nil {
			dbName = string(st.Name.SchemaName)
		}
	case *tree.CreateSequence:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Name != nil {
			dbName = string(st.Name.SchemaName)
		}
	case *tree.AlterView:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeAlterView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Name != nil {
			dbName = string(st.Name.SchemaName)
		}
	case *tree.AlterDataBaseConfig:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.CreateFunction:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true

	case *tree.AlterTable:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeAlterTable, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Table != nil {
			dbName = string(st.Table.SchemaName)
		}
	case *tree.CreateProcedure:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.CallStmt: // TODO: redesign privilege for calling a procedure
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.DropTable:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeDropTable, PrivilegeTypeDropObject, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if len(st.Names) != 0 {
			dbName = string(st.Names[0].SchemaName)
		}
	case *tree.DropView:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeDropView, PrivilegeTypeDropObject, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if len(st.Names) != 0 {
			dbName = string(st.Names[0].SchemaName)
		}
	case *tree.DropSequence:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeDropObject, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
		if len(st.Names) != 0 {
			dbName = string(st.Names[0].SchemaName)
		}
	case *tree.DropFunction:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.DropProcedure:
		objType = objectTypeDatabase
		typs = append(typs, PrivilegeTypeCreateView, PrivilegeTypeDatabaseAll, PrivilegeTypeDatabaseOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.Select, *tree.Do:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeSelect, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
	case *tree.Insert:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeInsert, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.Replace:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeInsert, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.Load:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeInsert, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Table != nil {
			dbName = string(st.Table.SchemaName)
		}
	case *tree.Update:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeUpdate, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.Delete:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeDelete, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.CreateIndex, *tree.DropIndex:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeIndex, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
	case *tree.ShowProcessList, *tree.ShowErrors, *tree.ShowWarnings, *tree.ShowVariables,
		*tree.ShowStatus, *tree.ShowTarget, *tree.ShowTableStatus,
		*tree.ShowGrants, *tree.ShowCollation, *tree.ShowIndex,
		*tree.ShowTableNumber, *tree.ShowColumnNumber,
		*tree.ShowTableValues, *tree.ShowNodeList, *tree.ShowRolesStmt,
		*tree.ShowLocks, *tree.ShowFunctionOrProcedureStatus, *tree.ShowPublications, *tree.ShowSubscriptions,
		*tree.ShowBackendServers:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.ShowAccounts:
		objType = objectTypeNone
		kind = privilegeKindSpecial
		special = specialTagAdmin
	case *tree.ExplainFor, *tree.ExplainAnalyze, *tree.ExplainStmt:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.BeginTransaction, *tree.CommitTransaction, *tree.RollbackTransaction, *tree.SetVar:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.SetDefaultRole, *tree.SetRole, *tree.SetPassword:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.PrepareStmt, *tree.PrepareString, *tree.Deallocate, *tree.Reset:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.Execute:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.Declare:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *InternalCmdFieldList:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.ValuesStatement:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeValues, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
	case *tree.TruncateTable:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeTruncate, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
		writeDatabaseAndTableDirectly = true
		if st.Name != nil {
			dbName = string(st.Name.SchemaName)
		}
	case *tree.MoDump:
		objType = objectTypeTable
		typs = append(typs, PrivilegeTypeSelect, PrivilegeTypeTableAll, PrivilegeTypeTableOwnership)
	case *tree.Kill:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.LockTableStmt, *tree.UnLockTableStmt:
		objType = objectTypeNone
		kind = privilegeKindNone
	case *tree.CreatePublication, *tree.DropPublication, *tree.AlterPublication:
		typs = append(typs, PrivilegeTypeAccountAll)
		objType = objectTypeDatabase
		kind = privilegeKindNone
	case *tree.SetTransaction:
		objType = objectTypeNone
		kind = privilegeKindNone
	default:
		panic(fmt.Sprintf("does not have the privilege definition of the statement %s", stmt))
	}

	entries := make([]privilegeEntry, len(typs))
	for i, typ := range typs {
		entries[i] = privilegeEntriesMap[typ]
		entries[i].databaseName = dbName
	}
	entries = append(entries, extraEntries...)
	return &privilege{
		kind:                          kind,
		objType:                       objType,
		entries:                       entries,
		special:                       special,
		writeDatabaseAndTableDirectly: writeDatabaseAndTableDirectly,
		isClusterTable:                clusterTable,
		clusterTableOperation:         clusterTableOperation}
}

// privilege will be done on the table
type privilegeTips struct {
	typ                   PrivilegeType
	databaseName          string
	tableName             string
	isClusterTable        bool
	clusterTableOperation clusterTableOperationType
}

type privilegeTipsArray []privilegeTips

func (pot privilegeTips) String() string {
	return fmt.Sprintf("%s %s %s", pot.typ, pot.databaseName, pot.tableName)
}

func (pota privilegeTipsArray) String() string {
	b := strings.Builder{}
	for _, table := range pota {
		b.WriteString(table.String())
		b.WriteString("\n")
	}
	return b.String()
}

// extractPrivilegeTipsFromPlan extracts the privilege tips from the plan
func extractPrivilegeTipsFromPlan(p *plan2.Plan) privilegeTipsArray {
	//NOTE: the pts may be nil when the plan does operate any table.
	var pts privilegeTipsArray
	appendPt := func(pt privilegeTips) {
		pts = append(pts, pt)
	}
	if p.GetQuery() != nil { //select,insert select, update, delete
		q := p.GetQuery()

		// lastNode := q.Nodes[len(q.Nodes)-1]
		var t PrivilegeType
		var clusterTable bool
		var clusterTableOperation clusterTableOperationType

		switch q.StmtType {
		case plan.Query_UPDATE:
			t = PrivilegeTypeUpdate
			clusterTableOperation = clusterTableModify
		case plan.Query_DELETE:
			t = PrivilegeTypeDelete
			clusterTableOperation = clusterTableModify
		case plan.Query_INSERT:
			t = PrivilegeTypeInsert
			clusterTableOperation = clusterTableModify
		default:
			t = PrivilegeTypeSelect
			clusterTableOperation = clusterTableSelect
		}

		for _, node := range q.Nodes {
			if node.NodeType == plan.Node_TABLE_SCAN {
				if node.ObjRef != nil {
					if node.TableDef != nil && node.TableDef.TableType == catalog.SystemClusterRel {
						clusterTable = true
					} else {
						clusterTable = isClusterTable(node.ObjRef.GetSchemaName(), node.ObjRef.GetObjName())
					}

					var scanTyp PrivilegeType
					switch q.StmtType {
					case plan.Query_UPDATE:
						scanTyp = PrivilegeTypeUpdate
						clusterTableOperation = clusterTableModify
					case plan.Query_DELETE:
						scanTyp = PrivilegeTypeDelete
						clusterTableOperation = clusterTableModify
					default:
						scanTyp = PrivilegeTypeSelect
						clusterTableOperation = clusterTableSelect
					}

					//do not check the privilege of the index table
					if !isIndexTable(node.ObjRef.GetObjName()) {
						appendPt(privilegeTips{
							typ:                   scanTyp,
							databaseName:          node.ObjRef.GetSchemaName(),
							tableName:             node.ObjRef.GetObjName(),
							isClusterTable:        clusterTable,
							clusterTableOperation: clusterTableOperation,
						})
					}
				}
			} else if node.NodeType == plan.Node_INSERT {
				if node.InsertCtx != nil && node.InsertCtx.Ref != nil {
					objRef := node.InsertCtx.Ref
					//do not check the privilege of the index table
					if !isIndexTable(node.ObjRef.GetObjName()) {
						appendPt(privilegeTips{
							typ:                   t,
							databaseName:          objRef.GetSchemaName(),
							tableName:             objRef.GetObjName(),
							isClusterTable:        node.InsertCtx.IsClusterTable,
							clusterTableOperation: clusterTableModify,
						})
					}
				}
			} else if node.NodeType == plan.Node_DELETE {
				if node.DeleteCtx != nil && node.DeleteCtx.Ref != nil {
					objRef := node.DeleteCtx.Ref
					//do not check the privilege of the index table
					if !isIndexTable(node.ObjRef.GetObjName()) {
						appendPt(privilegeTips{
							typ:                   t,
							databaseName:          objRef.GetSchemaName(),
							tableName:             objRef.GetObjName(),
							isClusterTable:        node.DeleteCtx.IsClusterTable,
							clusterTableOperation: clusterTableModify,
						})
					}
				}
			}
		}
	} else if p.GetDdl() != nil {
		if p.GetDdl().GetTruncateTable() != nil {
			truncateTable := p.GetDdl().GetTruncateTable()
			appendPt(privilegeTips{
				typ:                   PrivilegeTypeTruncate,
				databaseName:          truncateTable.GetDatabase(),
				tableName:             truncateTable.GetTable(),
				isClusterTable:        truncateTable.GetClusterTable().GetIsClusterTable(),
				clusterTableOperation: clusterTableModify,
			})
		} else if p.GetDdl().GetDropTable() != nil {
			dropTable := p.GetDdl().GetDropTable()
			appendPt(privilegeTips{
				typ:                   PrivilegeTypeDropTable,
				databaseName:          dropTable.GetDatabase(),
				tableName:             dropTable.GetTable(),
				isClusterTable:        dropTable.GetClusterTable().GetIsClusterTable(),
				clusterTableOperation: clusterTableDrop,
			})
		}
	}
	return pts
}

// convertPrivilegeTipsToPrivilege constructs the privilege entries from the privilege tips from the plan
func convertPrivilegeTipsToPrivilege(priv *privilege, arr privilegeTipsArray) {
	//rewirte the privilege entries based on privilege tips
	if priv.objectType() != objectTypeTable &&
		priv.objectType() != objectTypeDatabase {
		return
	}

	//NOTE: when the arr is nil, it denotes that there is no operation on the table.

	type pair struct {
		databaseName string
		tableName    string
	}

	dedup := make(map[pair]int8)

	//multi privileges take effect together
	entries := make([]privilegeEntry, 0, len(arr))
	multiPrivs := make([]privilegeItem, 0, len(arr))
	for _, tips := range arr {
		multiPrivs = append(multiPrivs, privilegeItem{
			privilegeTyp:          tips.typ,
			dbName:                tips.databaseName,
			tableName:             tips.tableName,
			isClusterTable:        tips.isClusterTable,
			clusterTableOperation: tips.clusterTableOperation,
		})

		dedup[pair{tips.databaseName, tips.tableName}] = 1
	}

	me := &compoundEntry{multiPrivs}
	entries = append(entries, privilegeEntry{privilegeEntryTyp: privilegeEntryTypeCompound, compound: me})

	//optional predefined privilege : tableAll, ownership
	predefined := []PrivilegeType{PrivilegeTypeTableAll, PrivilegeTypeTableOwnership}
	for _, p := range predefined {
		for par := range dedup {
			e := privilegeEntriesMap[p]
			e.databaseName = par.databaseName
			e.tableName = par.tableName
			entries = append(entries, e)
		}
	}

	priv.entries = entries
}

// getSqlFromPrivilegeEntry generates the query sql for the privilege entry
func getSqlFromPrivilegeEntry(ctx context.Context, roleId int64, entry privilegeEntry) (string, error) {
	var err error
	var sql string
	//for object type table, need concrete tableid
	//TODO: table level check should be done after getting the plan
	if entry.objType == objectTypeTable {
		switch entry.privilegeLevel {
		case privilegeLevelDatabaseTable, privilegeLevelTable:
			sql, err = getSqlForCheckRoleHasTableLevelPrivilege(ctx, roleId, entry.privilegeId, entry.databaseName, entry.tableName)
		case privilegeLevelDatabaseStar, privilegeLevelStar:
			sql, err = getSqlForCheckRoleHasTableLevelForDatabaseStar(ctx, roleId, entry.privilegeId, entry.databaseName)
		case privilegeLevelStarStar:
			sql = getSqlForCheckRoleHasTableLevelForStarStar(roleId, entry.privilegeId)
		default:
			return "", moerr.NewInternalError(ctx, "unsupported privilegel level %s for the privilege %s", entry.privilegeLevel, entry.privilegeId)
		}
	} else if entry.objType == objectTypeDatabase {
		switch entry.privilegeLevel {
		case privilegeLevelStar, privilegeLevelStarStar:
			sql = getSqlForCheckRoleHasDatabaseLevelForStarStar(roleId, entry.privilegeId, entry.privilegeLevel)
		case privilegeLevelDatabase:
			sql, err = getSqlForCheckRoleHasDatabaseLevelForDatabase(ctx, roleId, entry.privilegeId, entry.databaseName)
		default:
			return "", moerr.NewInternalError(ctx, "unsupported privilegel level %s for the privilege %s", entry.privilegeLevel, entry.privilegeId)
		}
	} else if entry.objType == objectTypeAccount {
		switch entry.privilegeLevel {
		case privilegeLevelStar:
			sql = getSqlForCheckRoleHasAccountLevelForStar(roleId, entry.privilegeId)
		default:
			return "false", moerr.NewInternalError(ctx, "unsupported privilegel level %s for the privilege %s", entry.privilegeLevel, entry.privilegeId)
		}
	} else {
		sql = getSqlForCheckRoleHasPrivilege(roleId, entry.objType, int64(entry.objId), int64(entry.privilegeId))
	}
	return sql, err
}

// getPrivilegeLevelsOfObjectType gets the privilege levels of the objectType
func getPrivilegeLevelsOfObjectType(ctx context.Context, objType objectType) ([]privilegeLevelType, error) {
	if ret, ok := objectType2privilegeLevels[objType]; ok {
		return ret, nil
	}
	return nil, moerr.NewInternalError(ctx, "do not support the object type %s", objType.String())
}

// getSqlForPrivilege generates the query sql for the privilege entry
func getSqlForPrivilege(ctx context.Context, roleId int64, entry privilegeEntry, pl privilegeLevelType) (string, error) {
	var sql string
	var err error
	//for object type table, need concrete tableid
	switch entry.objType {
	case objectTypeTable:
		switch pl {
		case privilegeLevelDatabaseTable, privilegeLevelTable:
			sql, err = getSqlForCheckRoleHasTableLevelPrivilege(ctx, roleId, entry.privilegeId, entry.databaseName, entry.tableName)
		case privilegeLevelDatabaseStar, privilegeLevelStar:
			sql, err = getSqlForCheckRoleHasTableLevelForDatabaseStar(ctx, roleId, entry.privilegeId, entry.databaseName)
		case privilegeLevelStarStar:
			sql = getSqlForCheckRoleHasTableLevelForStarStar(roleId, entry.privilegeId)
		default:
			return "", moerr.NewInternalError(ctx, "the privilege level %s for the privilege %s is unsupported", pl, entry.privilegeId)
		}
	case objectTypeDatabase:
		switch pl {
		case privilegeLevelStar, privilegeLevelStarStar:
			sql = getSqlForCheckRoleHasDatabaseLevelForStarStar(roleId, entry.privilegeId, pl)
		case privilegeLevelDatabase:
			sql, err = getSqlForCheckRoleHasDatabaseLevelForDatabase(ctx, roleId, entry.privilegeId, entry.databaseName)
		default:
			return "", moerr.NewInternalError(ctx, "the privilege level %s for the privilege %s is unsupported", pl, entry.privilegeId)
		}
	case objectTypeAccount:
		switch pl {
		case privilegeLevelStar:
			sql = getSqlForCheckRoleHasAccountLevelForStar(roleId, entry.privilegeId)
		default:
			return "false", moerr.NewInternalError(ctx, "the privilege level %s for the privilege %s is unsupported", pl, entry.privilegeId)
		}
	default:
		sql = getSqlForCheckRoleHasPrivilege(roleId, entry.objType, int64(entry.objId), int64(entry.privilegeId))
	}

	return sql, err
}

// getSqlForPrivilege2 complements the database name and calls getSqlForPrivilege
func getSqlForPrivilege2(ses *Session, roleId int64, entry privilegeEntry, pl privilegeLevelType) (string, error) {
	//handle the empty database
	if len(entry.databaseName) == 0 {
		entry.databaseName = ses.GetDatabaseName()
	}
	return getSqlForPrivilege(ses.GetRequestContext(), roleId, entry, pl)
}

// verifyPrivilegeEntryInMultiPrivilegeLevels checks the privilege
// with multi-privilege levels exists or not
func verifyPrivilegeEntryInMultiPrivilegeLevels(
	ctx context.Context,
	bh BackgroundExec,
	ses *Session,
	cache *privilegeCache,
	roleId int64,
	entry privilegeEntry,
	pls []privilegeLevelType,
	enableCache bool) (bool, error) {
	var erArray []ExecResult
	var sql string
	var yes bool
	var err error
	dbName := entry.databaseName
	if len(dbName) == 0 {
		dbName = ses.GetDatabaseName()
	}
	for _, pl := range pls {
		if cache != nil && enableCache {
			yes = cache.has(entry.objType, pl, dbName, entry.tableName, entry.privilegeId)
			if yes {
				return true, nil
			}
		}
		sql, err = getSqlForPrivilege2(ses, roleId, entry, pl)
		if err != nil {
			return false, err
		}

		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return false, err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return false, err
		}

		if execResultArrayHasData(erArray) {
			if cache != nil && enableCache {
				cache.add(entry.objType, pl, dbName, entry.tableName, entry.privilegeId)
			}
			return true, nil
		}
	}
	return false, nil
}

// determineRoleSetHasPrivilegeSet decides the role set has at least one privilege of the privilege set.
// The algorithm 2.
func determineRoleSetHasPrivilegeSet(ctx context.Context, bh BackgroundExec, ses *Session, roleIds *btree.Set[int64], priv *privilege, enableCache bool) (bool, error) {
	var err error
	var pls []privilegeLevelType

	var yes bool
	var yes2 bool
	//there is no privilege needs, just approve
	if len(priv.entries) == 0 {
		return false, nil
	}

	cache := ses.GetPrivilegeCache()

	for _, roleId := range roleIds.Keys() {
		for _, entry := range priv.entries {
			if entry.privilegeEntryTyp == privilegeEntryTypeGeneral {
				pls, err = getPrivilegeLevelsOfObjectType(ctx, entry.objType)
				if err != nil {
					return false, err
				}

				yes2 = verifyLightPrivilege(ses,
					entry.databaseName,
					priv.writeDatabaseAndTableDirectly,
					priv.isClusterTable,
					priv.clusterTableOperation)

				if yes2 {
					yes, err = verifyPrivilegeEntryInMultiPrivilegeLevels(ctx, bh, ses, cache, roleId, entry, pls, enableCache)
					if err != nil {
						return false, err
					}
				}

				if yes {
					return true, nil
				}
			} else if entry.privilegeEntryTyp == privilegeEntryTypeCompound {
				if entry.compound != nil {
					allTrue := true
					//multi privileges take effect together
					for _, mi := range entry.compound.items {
						if mi.privilegeTyp == PrivilegeTypeCanGrantRoleToOthersInCreateUser {
							//TODO: normalize the name
							//TODO: simplify the logic
							yes, err = determineUserCanGrantRolesToOthersInternal(ctx, bh, ses, []*tree.Role{mi.role})
							if err != nil {
								return false, err
							}
							if yes {
								from := &verifiedRole{
									typ:  roleType,
									name: mi.role.UserName,
								}
								for _, user := range mi.users {
									to := &verifiedRole{
										typ:  userType,
										name: user.Username,
									}
									err = verifySpecialRolesInGrant(ctx, ses.GetTenantInfo(), from, to)
									if err != nil {
										return false, err
									}
								}
							}
						} else {
							tempEntry := privilegeEntriesMap[mi.privilegeTyp]
							tempEntry.databaseName = mi.dbName
							tempEntry.tableName = mi.tableName
							tempEntry.privilegeEntryTyp = privilegeEntryTypeGeneral
							tempEntry.compound = nil
							pls, err = getPrivilegeLevelsOfObjectType(ctx, tempEntry.objType)
							if err != nil {
								return false, err
							}

							yes2 = verifyLightPrivilege(ses,
								tempEntry.databaseName,
								priv.writeDatabaseAndTableDirectly,
								mi.isClusterTable,
								mi.clusterTableOperation)

							if yes2 {
								//At least there is one success
								yes, err = verifyPrivilegeEntryInMultiPrivilegeLevels(ctx, bh, ses, cache, roleId, tempEntry, pls, enableCache)
								if err != nil {
									return false, err
								}
							}
						}
						if !yes {
							allTrue = false
							break
						}
					}

					if allTrue {
						return allTrue, nil
					}
				}
			}
		}
	}
	return false, nil
}

// determineUserHasPrivilegeSet decides the privileges of user can satisfy the requirement of the privilege set
// The algorithm 1.
func determineUserHasPrivilegeSet(ctx context.Context, ses *Session, priv *privilege, stmt tree.Statement) (ret bool, err error) {
	var erArray []ExecResult
	var yes bool
	var roleB int64
	var ok bool
	var grantedIds *btree.Set[int64]
	var enableCache bool

	//check privilege cache first
	if len(priv.entries) == 0 {
		return false, nil
	}

	enableCache, err = privilegeCacheIsEnabled(ses)
	if err != nil {
		return false, err
	}
	if enableCache {
		yes, err = checkPrivilegeInCache(ctx, ses, priv, enableCache)
		if err != nil {
			return false, err
		}
		if yes {
			return true, nil
		}
	}

	tenant := ses.GetTenantInfo()
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//the set of roles the (k+1) th iteration during the execution
	roleSetOfKPlusOneThIteration := &btree.Set[int64]{}
	//the set of roles the k th iteration during the execution
	roleSetOfKthIteration := &btree.Set[int64]{}
	//the set of roles visited by traversal algorithm
	roleSetOfVisited := &btree.Set[int64]{}
	//simple mo_role_grant cache
	cacheOfMoRoleGrant := &btree.Map[int64, *btree.Set[int64]]{}

	//step 1: The Set R1 {default role id}
	//The primary role (in use)
	roleSetOfKthIteration.Insert((int64)(tenant.GetDefaultRoleID()))

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return false, err
	}

	//step 2: The Set R2 {the roleid granted to the userid}
	//If the user uses the all secondary roles, the secondary roles needed to be loaded
	err = loadAllSecondaryRoles(ctx, bh, tenant, roleSetOfKthIteration)
	if err != nil {
		return false, err
	}

	//init RVisited = Rk
	roleSetOfKthIteration.Scan(func(roleId int64) bool {
		roleSetOfVisited.Insert(roleId)
		return true
	})

	//Call the algorithm 2.
	//If the result of the algorithm 2 is true, Then return true;
	yes, err = determineRoleSetHasPrivilegeSet(ctx, bh, ses, roleSetOfKthIteration, priv, enableCache)
	if err != nil {
		return false, err
	}
	if yes {
		ret = true
		return ret, err
	}
	/*
		step 3: !!!NOTE all roleid in Rk has been processed by the algorithm 2.
		RVisited is the set of all roleid that has been processed.
		RVisited = Rk;
		For {
			For roleA in Rk {
				Find the peer roleB in the table mo_role_grant(granted_id,grantee_id) with grantee_id = roleA;
				If roleB is not in RVisited, Then add roleB into R(k+1);
					add roleB into RVisited;
			}

			If R(k+1) is empty, Then return false;
			//Call the algorithm 2.
			If the result of the algorithm 2 is true, Then return true;
			Rk = R(k+1);
			R(k+1) = {};
		}
	*/
	for {
		quit := false
		select {
		case <-ctx.Done():
			quit = true
		default:
		}
		if quit {
			break
		}

		roleSetOfKPlusOneThIteration.Clear()

		//get roleB of roleA
		for _, roleA := range roleSetOfKthIteration.Keys() {
			if grantedIds, ok = cacheOfMoRoleGrant.Get(roleA); ok {
				for _, grantedId := range grantedIds.Keys() {
					roleSetOfKPlusOneThIteration.Insert(grantedId)
				}
				continue
			}
			grantedIds = &btree.Set[int64]{}
			cacheOfMoRoleGrant.Set(roleA, grantedIds)
			sqlForInheritedRoleIdOfRoleId := getSqlForInheritedRoleIdOfRoleId(roleA)
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sqlForInheritedRoleIdOfRoleId)
			if err != nil {
				return false, moerr.NewInternalError(ctx, "get inherited role id of the role id. error:%v", err)
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return false, err
			}

			if execResultArrayHasData(erArray) {
				for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
					roleB, err = erArray[0].GetInt64(ctx, i, 0)
					if err != nil {
						return false, err
					}

					if !roleSetOfVisited.Contains(roleB) {
						roleSetOfVisited.Insert(roleB)
						roleSetOfKPlusOneThIteration.Insert(roleB)
						grantedIds.Insert(roleB)
					}
				}
			}
		}

		//no more roleB, it is done
		if roleSetOfKPlusOneThIteration.Len() == 0 {
			ret = false
			return ret, err
		}

		//Call the algorithm 2.
		//If the result of the algorithm 2 is true, Then return true;
		yes, err = determineRoleSetHasPrivilegeSet(ctx, bh, ses, roleSetOfKPlusOneThIteration, priv, enableCache)
		if err != nil {
			return false, err
		}

		if yes {
			ret = true
			return ret, err
		}
		roleSetOfKthIteration, roleSetOfKPlusOneThIteration = roleSetOfKPlusOneThIteration, roleSetOfKthIteration
	}
	return ret, err
}

const (
	goOn        int = iota
	successDone     //ri has indirect relation with the Uc
)

// loadAllSecondaryRoles loads all secondary roles
func loadAllSecondaryRoles(ctx context.Context, bh BackgroundExec, account *TenantInfo, roleSetOfCurrentUser *btree.Set[int64]) error {
	var err error
	var sql string

	var erArray []ExecResult
	var roleId int64

	if account.GetUseSecondaryRole() {
		sql = getSqlForRoleIdOfUserId(int(account.GetUserID()))
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if execResultArrayHasData(erArray) {
			for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
				roleId, err = erArray[0].GetInt64(ctx, i, 0)
				if err != nil {
					return err
				}
				roleSetOfCurrentUser.Insert(roleId)
			}
		}
	}
	return err
}

// determineUserCanGrantRolesToOthersInternal decides if the user can grant roles to other users or roles
// the same as the grant/revoke privilege, role with inputted transaction and BackgroundExec
func determineUserCanGrantRolesToOthersInternal(ctx context.Context, bh BackgroundExec, ses *Session, fromRoles []*tree.Role) (bool, error) {
	//step1: normalize the names of roles and users
	var err error
	err = normalizeNamesOfRoles(ctx, fromRoles)
	if err != nil {
		return false, err
	}

	//step2: decide the current user
	account := ses.GetTenantInfo()

	//step3: check the link: roleX -> roleA -> .... -> roleZ -> the current user. Every link has the with_grant_option.
	var vr *verifiedRole
	var ret = true
	var granted bool
	//the temporal set of roles during the execution
	var tempRoleSet *btree.Set[int64]
	var sql string
	//the set of roles the (k+1) th iteration during the execution
	roleSetOfKPlusOneThIteration := &btree.Set[int64]{}
	//the set of roles the k th iteration during the execution
	roleSetOfKthIteration := &btree.Set[int64]{}
	//the set of roles of the current user that executes this statement or function
	roleSetOfCurrentUser := &btree.Set[int64]{}
	//the set of roles visited by traversal algorithm
	roleSetOfVisited := &btree.Set[int64]{}
	verifiedFromRoles := make([]*verifiedRole, len(fromRoles))

	//step 1 : add the primary role
	roleSetOfCurrentUser.Insert(int64(account.GetDefaultRoleID()))

	for i, role := range fromRoles {
		sql, err = getSqlForRoleIdOfRole(ctx, role.UserName)
		if err != nil {
			return false, err
		}
		vr, err = verifyRoleFunc(ctx, bh, sql, role.UserName, roleType)
		if err != nil {
			return false, err
		}
		if vr == nil {
			return false, moerr.NewInternalError(ctx, "there is no role %s", role.UserName)
		}
		verifiedFromRoles[i] = vr
	}

	//step 2: The Set R2 {the roleid granted to the userid}
	//If the user uses the all secondary roles, the secondary roles needed to be loaded
	err = loadAllSecondaryRoles(ctx, bh, account, roleSetOfCurrentUser)
	if err != nil {
		return false, err
	}

	for _, role := range verifiedFromRoles {
		//if it is the role in use, do the check
		if roleSetOfCurrentUser.Contains(role.id) {
			//check the direct relation between role and user
			granted, err = isRoleGrantedToUserWGO(ctx, bh, role.id, int64(account.GetUserID()))
			if err != nil {
				return false, err
			}
			if granted {
				continue
			}
		}

		roleSetOfKthIteration.Clear()
		roleSetOfVisited.Clear()
		roleSetOfKthIteration.Insert(role.id)

		riResult := goOn
		//It is kind of level traversal
		for roleSetOfKthIteration.Len() != 0 && riResult == goOn {
			roleSetOfKPlusOneThIteration.Clear()
			for _, ri := range roleSetOfKthIteration.Keys() {
				tempRoleSet, err = getRoleSetThatRoleGrantedToWGO(ctx, bh, ri, roleSetOfVisited, roleSetOfKPlusOneThIteration)
				if err != nil {
					return false, err
				}

				if setIsIntersected(tempRoleSet, roleSetOfCurrentUser) {
					riResult = successDone
					break
				}
			}

			//swap Rk,R(k+1)
			roleSetOfKthIteration, roleSetOfKPlusOneThIteration = roleSetOfKPlusOneThIteration, roleSetOfKthIteration
		}

		if riResult != successDone {
			//fail
			ret = false
			break
		}
	}
	return ret, err
}

// determineUserCanGrantRoleToOtherUsers decides if the user can grant roles to other users or roles
// the same as the grant/revoke privilege, role.
func determineUserCanGrantRolesToOthers(ctx context.Context, ses *Session, fromRoles []*tree.Role) (ret bool, err error) {
	//step1: normalize the names of roles and users
	err = normalizeNamesOfRoles(ctx, fromRoles)
	if err != nil {
		return false, err
	}

	//step2: decide the current user
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return false, err
	}

	ret, err = determineUserCanGrantRolesToOthersInternal(ctx, bh, ses, fromRoles)
	if err != nil {
		return false, err
	}

	return ret, err
}

// isRoleGrantedToUserWGO verifies the role has been granted to the user with with_grant_option = true.
// Algorithm 1
func isRoleGrantedToUserWGO(ctx context.Context, bh BackgroundExec, roleId, UserId int64) (bool, error) {
	var err error
	var erArray []ExecResult
	sql := getSqlForCheckUserGrantWGO(roleId, UserId)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return false, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}

	if execResultArrayHasData(erArray) {
		return true, nil
	}

	return false, nil
}

// getRoleSetThatRoleGrantedToWGO returns all the roles that the role has been granted to with with_grant_option = true.
// Algorithm 2
func getRoleSetThatRoleGrantedToWGO(ctx context.Context, bh BackgroundExec, roleId int64, RVisited, RkPlusOne *btree.Set[int64]) (*btree.Set[int64], error) {
	var err error
	var erArray []ExecResult
	var id int64
	rset := &btree.Set[int64]{}
	sql := getSqlForCheckRoleGrantWGO(roleId)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, err
	}

	if execResultArrayHasData(erArray) {
		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			id, err = erArray[0].GetInt64(ctx, i, 0)
			if err != nil {
				return nil, err
			}
			if !RVisited.Contains(id) {
				RVisited.Insert(id)
				RkPlusOne.Insert(id)
				rset.Insert(id)
			}
		}
	}

	return rset, err
}

// authenticateUserCanExecuteStatementWithObjectTypeAccountAndDatabase decides the user has the privilege of executing the statement with object type account
func authenticateUserCanExecuteStatementWithObjectTypeAccountAndDatabase(ctx context.Context, ses *Session, stmt tree.Statement) (bool, error) {
	var err error
	var ok, yes bool
	priv := ses.GetPrivilege()
	if priv.objectType() != objectTypeAccount && priv.objectType() != objectTypeDatabase { //do nothing
		return true, nil
	}
	ok, err = determineUserHasPrivilegeSet(ctx, ses, priv, stmt)
	if err != nil {
		return false, err
	}

	//double check privilege of drop table
	if !ok && ses.GetFromRealUser() && ses.GetTenantInfo() != nil && ses.GetTenantInfo().IsSysTenant() {
		switch dropTable := stmt.(type) {
		case *tree.DropTable:
			dbName := string(dropTable.Names[0].SchemaName)
			if len(dbName) == 0 {
				dbName = ses.GetDatabaseName()
			}
			return isClusterTable(dbName, string(dropTable.Names[0].ObjectName)), nil
		}
	}

	//for GrantRole statement, check with_grant_option
	if !ok && priv.kind == privilegeKindInherit {
		grant := stmt.(*tree.Grant)
		grantRole := grant.GrantRole
		yes, err = determineUserCanGrantRolesToOthers(ctx, ses, grantRole.Roles)
		if err != nil {
			return false, err
		}
		if yes {
			return true, nil
		}
	}
	//for Create User statement with default role.
	//TODO:

	// support dropdatabase and droptable for owner
	if !ok && ses.GetFromRealUser() && ses.GetTenantInfo() != nil && priv.kind == privilegeKindGeneral {
		switch st := stmt.(type) {
		case *tree.DropDatabase:
			// get the databasename
			dbName := string(st.Name)
			if _, inSet := sysDatabases[dbName]; inSet {
				return ok, nil
			}
			return checkRoleWhetherDatabaseOwner(ctx, ses, dbName, ok)
		case *tree.DropTable:
			// get the databasename and tablename
			if len(st.Names) != 1 {
				return ok, nil
			}
			dbName := string(st.Names[0].SchemaName)
			if len(dbName) == 0 {
				dbName = ses.GetDatabaseName()
			}
			if _, inSet := sysDatabases[dbName]; inSet {
				return ok, nil
			}
			tbName := string(st.Names[0].ObjectName)
			return checkRoleWhetherTableOwner(ctx, ses, dbName, tbName, ok)
		}
	}
	return ok, nil
}

func checkRoleWhetherTableOwner(ctx context.Context, ses *Session, dbName, tbName string, ok bool) (bool, error) {
	var owner int64
	var err error
	var erArray []ExecResult
	var sql string
	roles := make([]int64, 0)
	tenantInfo := ses.GetTenantInfo()
	// current user
	currentUser := tenantInfo.GetUserID()

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	// getOwner of the table
	sql = getSqlForGetOwnerOfTable(dbName, tbName)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return ok, nil
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return ok, nil
	}

	if execResultArrayHasData(erArray) {
		owner, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return ok, nil
		}
	} else {
		return ok, nil
	}

	// check role
	if tenantInfo.useAllSecondaryRole {
		sql = getSqlForGetRolesOfCurrentUser(int64(currentUser))
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return ok, nil
		}
		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return ok, nil
		}

		if execResultArrayHasData(erArray) {
			for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
				role, err := erArray[0].GetInt64(ctx, i, 0)
				if err != nil {
					return ok, nil
				}
				roles = append(roles, role)
			}
		} else {
			return ok, nil
		}

		// check the role whether the table's owner
		for _, role := range roles {
			if role == owner {
				return true, nil
			}
		}
	} else {
		currentRole := tenantInfo.GetDefaultRoleID()
		if owner == int64(currentRole) {
			return true, nil
		}
	}
	return ok, nil

}

func checkRoleWhetherDatabaseOwner(ctx context.Context, ses *Session, dbName string, ok bool) (bool, error) {
	var owner int64
	var err error
	var erArray []ExecResult
	var sql string
	roles := make([]int64, 0)

	tenantInfo := ses.GetTenantInfo()
	// current user
	currentUser := tenantInfo.GetUserID()

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	// getOwner of the database
	sql = getSqlForGetOwnerOfDatabase(dbName)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return ok, nil
	}
	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return ok, nil
	}

	if execResultArrayHasData(erArray) {
		owner, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return ok, nil
		}
	} else {
		return ok, nil
	}

	// check role
	if tenantInfo.useAllSecondaryRole {
		sql = getSqlForGetRolesOfCurrentUser(int64(currentUser))
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return ok, nil
		}
		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return ok, nil
		}

		if execResultArrayHasData(erArray) {
			for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
				role, err := erArray[0].GetInt64(ctx, i, 0)
				if err != nil {
					return ok, nil
				}
				roles = append(roles, role)
			}
		} else {
			return ok, nil
		}

		// check the role whether the database's owner
		for _, role := range roles {
			if role == owner {
				return true, nil
			}
		}
	} else {
		currentRole := tenantInfo.GetDefaultRoleID()
		if owner == int64(currentRole) {
			return true, nil
		}
	}
	return ok, nil
}

// authenticateUserCanExecuteStatementWithObjectTypeDatabaseAndTable
// decides the user has the privilege of executing the statement
// with object type table from the plan
func authenticateUserCanExecuteStatementWithObjectTypeDatabaseAndTable(ctx context.Context,
	ses *Session,
	stmt tree.Statement,
	p *plan2.Plan) (bool, error) {
	priv := determinePrivilegeSetOfStatement(stmt)
	if priv.objectType() == objectTypeTable {
		arr := extractPrivilegeTipsFromPlan(p)
		if len(arr) == 0 {
			return true, nil
		}
		convertPrivilegeTipsToPrivilege(priv, arr)
		ok, err := determineUserHasPrivilegeSet(ctx, ses, priv, stmt)
		if err != nil {
			return false, err
		}
		return ok, nil
	}
	return true, nil
}

// formSqlFromGrantPrivilege makes the sql for querying the database.
func formSqlFromGrantPrivilege(ctx context.Context, ses *Session, gp *tree.GrantPrivilege, priv *tree.Privilege) (string, error) {
	tenant := ses.GetTenantInfo()
	sql := ""
	var privType PrivilegeType
	var err error
	privType, err = convertAstPrivilegeTypeToPrivilegeType(ctx, priv.Type, gp.ObjType)
	if err != nil {
		return "", err
	}
	switch gp.ObjType {
	case tree.OBJECT_TYPE_TABLE:
		switch gp.Level.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			sql, err = getSqlForCheckWithGrantOptionForTableDatabaseStar(ctx, int64(tenant.GetDefaultRoleID()), privType, ses.GetDatabaseName())
		case tree.PRIVILEGE_LEVEL_TYPE_STAR_STAR:
			sql = getSqlForCheckWithGrantOptionForTableStarStar(int64(tenant.GetDefaultRoleID()), privType)
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE_STAR:
			sql, err = getSqlForCheckWithGrantOptionForTableDatabaseStar(ctx, int64(tenant.GetDefaultRoleID()), privType, gp.Level.DbName)
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE_TABLE:
			sql, err = getSqlForCheckWithGrantOptionForTableDatabaseTable(ctx, int64(tenant.GetDefaultRoleID()), privType, gp.Level.DbName, gp.Level.TabName)
		case tree.PRIVILEGE_LEVEL_TYPE_TABLE:
			sql, err = getSqlForCheckWithGrantOptionForTableDatabaseTable(ctx, int64(tenant.GetDefaultRoleID()), privType, ses.GetDatabaseName(), gp.Level.TabName)
		default:
			return "", moerr.NewInternalError(ctx, "in object type %v privilege level type %v is unsupported", gp.ObjType, gp.Level.Level)
		}
	case tree.OBJECT_TYPE_DATABASE:
		switch gp.Level.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			sql = getSqlForCheckWithGrantOptionForDatabaseStar(int64(tenant.GetDefaultRoleID()), privType)
		case tree.PRIVILEGE_LEVEL_TYPE_STAR_STAR:
			sql = getSqlForCheckWithGrantOptionForDatabaseStarStar(int64(tenant.GetDefaultRoleID()), privType)
		case tree.PRIVILEGE_LEVEL_TYPE_TABLE:
			//in the syntax, we can not distinguish the table name from the database name.
			sql, err = getSqlForCheckWithGrantOptionForDatabaseDB(ctx, int64(tenant.GetDefaultRoleID()), privType, gp.Level.TabName)
		case tree.PRIVILEGE_LEVEL_TYPE_DATABASE:
			sql, err = getSqlForCheckWithGrantOptionForDatabaseDB(ctx, int64(tenant.GetDefaultRoleID()), privType, gp.Level.DbName)
		default:
			return "", moerr.NewInternalError(ctx, "in object type %v privilege level type %v is unsupported", gp.ObjType, gp.Level.Level)
		}
	case tree.OBJECT_TYPE_ACCOUNT:
		switch gp.Level.Level {
		case tree.PRIVILEGE_LEVEL_TYPE_STAR:
			sql = getSqlForCheckWithGrantOptionForAccountStar(int64(tenant.GetDefaultRoleID()), privType)
		default:
			return "", moerr.NewInternalError(ctx, "in object type %v privilege level type %v is unsupported", gp.ObjType, gp.Level.Level)
		}
	default:
		return "", moerr.NewInternalError(ctx, "object type %v is unsupported", gp.ObjType)
	}
	return sql, err
}

// getSqlForCheckRoleHasPrivilegeWGODependsOnPrivType return getSqlForCheckRoleHasPrivilegeWGO denpends on the pritype
func getSqlForCheckRoleHasPrivilegeWGODependsOnPrivType(privType PrivilegeType) string {
	switch privType {
	// account level privleges
	case PrivilegeTypeCreateAccount:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeDropAccount:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeAlterAccount:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeCreateUser:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeDropUser:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeAlterUser:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeCreateRole:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeDropRole:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeAlterRole:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeCreateDatabase:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeDropDatabase:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeShowDatabases:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeConnect:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeManageGrants:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeAccountAll:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeAccountAll), int64(PrivilegeTypeAccountOwnership))
	case PrivilegeTypeAccountOwnership:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))

	// database level privileges
	case PrivilegeTypeShowTables:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeCreateTable:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeDropTable:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeCreateView:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeDropView:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeAlterView:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeAlterTable:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeDatabaseAll:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeDatabaseAll), int64(PrivilegeTypeDatabaseOwnership))
	case PrivilegeTypeDatabaseOwnership:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))

	// table level privileges
	case PrivilegeTypeSelect:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeInsert:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeUpdate:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeTruncate:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeDelete:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeReference:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeIndex:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeTableAll:
		return getSqlForCheckRoleHasPrivilegeWGOOrWithOwnerShip(int64(privType), int64(PrivilegeTypeTableAll), int64(PrivilegeTypeTableOwnership))
	case PrivilegeTypeTableOwnership:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))

	// other privileges
	case PrivilegeTypeExecute:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))
	case PrivilegeTypeValues:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))

	default:
		return getSqlForCheckRoleHasPrivilegeWGO(int64(privType))
	}

}

// getRoleSetThatPrivilegeGrantedToWGO gets all roles that the privilege granted to with with_grant_option = true
// The algorithm 3
func getRoleSetThatPrivilegeGrantedToWGO(ctx context.Context, bh BackgroundExec, privType PrivilegeType) (*btree.Set[int64], error) {
	var err error
	var erArray []ExecResult
	var id int64
	rset := &btree.Set[int64]{}
	sql := getSqlForCheckRoleHasPrivilegeWGODependsOnPrivType(privType)
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, err
	}

	if execResultArrayHasData(erArray) {
		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			id, err = erArray[0].GetInt64(ctx, i, 0)
			if err != nil {
				return nil, err
			}
			rset.Insert(id)
		}
	}

	return rset, err
}

// setIsIntersected decides the A is intersecting the B.
func setIsIntersected(A, B *btree.Set[int64]) bool {
	if A.Len() > B.Len() {
		A, B = B, A
	}
	iter := A.Iter()
	for x := iter.First(); x; x = iter.Next() {
		if B.Contains(iter.Key()) {
			return true
		}
	}
	return false
}

// determineUserCanGrantPrivilegesToOthers decides the privileges can be granted to others.
func determineUserCanGrantPrivilegesToOthers(ctx context.Context, ses *Session, gp *tree.GrantPrivilege) (ret bool, err error) {
	//step1: normalize the names of roles and users
	//step2: decide the current user
	account := ses.GetTenantInfo()
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//step3: check the link: roleX -> roleA -> .... -> roleZ -> the current user. Every link has the with_grant_option.
	ret = true
	var privType PrivilegeType
	//the temporal set of roles during the execution
	var tempRoleSet *btree.Set[int64]
	//the set of roles that the privilege granted to with WGO=true
	var roleSetOfPrivilegeGrantedToWGO *btree.Set[int64]
	//the set of roles the (k+1) th iteration during the execution
	roleSetOfKPlusOneThIteration := &btree.Set[int64]{}
	//the set of roles the k th iteration during the execution
	roleSetOfKthIteration := &btree.Set[int64]{}
	//the set of roles visited by traversal algorithm
	roleSetOfVisited := &btree.Set[int64]{}
	//the set of roles of the current user that executes this statement or function
	roleSetOfCurrentUser := &btree.Set[int64]{}

	roleSetOfCurrentUser.Insert(int64(account.GetDefaultRoleID()))

	//put it into the single transaction
	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return false, err
	}

	//step 2: The Set R2 {the roleid granted to the userid}
	//If the user uses the all secondary roles, the secondary roles needed to be loaded
	err = loadAllSecondaryRoles(ctx, bh, account, roleSetOfCurrentUser)
	if err != nil {
		return false, err
	}

	for _, priv := range gp.Privileges {
		privType, err = convertAstPrivilegeTypeToPrivilegeType(ctx, priv.Type, gp.ObjType)
		if err != nil {
			return false, err
		}

		//call the algorithm 3.
		roleSetOfPrivilegeGrantedToWGO, err = getRoleSetThatPrivilegeGrantedToWGO(ctx, bh, privType)
		if err != nil {
			return false, err
		}

		if setIsIntersected(roleSetOfPrivilegeGrantedToWGO, roleSetOfCurrentUser) {
			continue
		}

		riResult := goOn
		for _, rx := range roleSetOfPrivilegeGrantedToWGO.Keys() {
			roleSetOfKthIteration.Clear()
			roleSetOfVisited.Clear()
			roleSetOfKthIteration.Insert(rx)

			//It is kind of level traversal
			for roleSetOfKthIteration.Len() != 0 && riResult == goOn {
				roleSetOfKPlusOneThIteration.Clear()
				for _, ri := range roleSetOfKthIteration.Keys() {
					tempRoleSet, err = getRoleSetThatRoleGrantedToWGO(ctx, bh, ri, roleSetOfVisited, roleSetOfKPlusOneThIteration)
					if err != nil {
						return false, err
					}

					if setIsIntersected(tempRoleSet, roleSetOfCurrentUser) {
						riResult = successDone
						break
					}
				}

				//swap Rk,R(k+1)
				roleSetOfKthIteration, roleSetOfKPlusOneThIteration = roleSetOfKPlusOneThIteration, roleSetOfKthIteration
			}

			if riResult == successDone {
				break
			}
		}
		if riResult != successDone {
			ret = false
			break
		}
	}
	return ret, err
}

func convertAstPrivilegeTypeToPrivilegeType(ctx context.Context, priv tree.PrivilegeType, ot tree.ObjectType) (PrivilegeType, error) {
	var privType PrivilegeType
	switch priv {
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_ACCOUNT:
		privType = PrivilegeTypeCreateAccount
	case tree.PRIVILEGE_TYPE_STATIC_DROP_ACCOUNT:
		privType = PrivilegeTypeDropAccount
	case tree.PRIVILEGE_TYPE_STATIC_ALTER_ACCOUNT:
		privType = PrivilegeTypeAlterAccount
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_USER:
		privType = PrivilegeTypeCreateUser
	case tree.PRIVILEGE_TYPE_STATIC_DROP_USER:
		privType = PrivilegeTypeDropUser
	case tree.PRIVILEGE_TYPE_STATIC_ALTER_USER:
		privType = PrivilegeTypeAlterUser
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_ROLE:
		privType = PrivilegeTypeCreateRole
	case tree.PRIVILEGE_TYPE_STATIC_DROP_ROLE:
		privType = PrivilegeTypeDropRole
	case tree.PRIVILEGE_TYPE_STATIC_ALTER_ROLE:
		privType = PrivilegeTypeAlterRole
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_DATABASE:
		privType = PrivilegeTypeCreateDatabase
	case tree.PRIVILEGE_TYPE_STATIC_DROP_DATABASE:
		privType = PrivilegeTypeDropDatabase
	case tree.PRIVILEGE_TYPE_STATIC_SHOW_DATABASES:
		privType = PrivilegeTypeShowDatabases
	case tree.PRIVILEGE_TYPE_STATIC_CONNECT:
		privType = PrivilegeTypeConnect
	case tree.PRIVILEGE_TYPE_STATIC_MANAGE_GRANTS:
		privType = PrivilegeTypeManageGrants
	case tree.PRIVILEGE_TYPE_STATIC_ALL:
		switch ot {
		case tree.OBJECT_TYPE_ACCOUNT:
			privType = PrivilegeTypeAccountAll
		case tree.OBJECT_TYPE_DATABASE:
			privType = PrivilegeTypeDatabaseAll
		case tree.OBJECT_TYPE_TABLE:
			privType = PrivilegeTypeTableAll
		default:
			return 0, moerr.NewInternalError(ctx, `the object type "%s" do not support the privilege "%s"`, ot.String(), priv.ToString())
		}
	case tree.PRIVILEGE_TYPE_STATIC_OWNERSHIP:
		switch ot {
		case tree.OBJECT_TYPE_DATABASE:
			privType = PrivilegeTypeDatabaseOwnership
		case tree.OBJECT_TYPE_TABLE:
			privType = PrivilegeTypeTableOwnership
		default:
			return 0, moerr.NewInternalError(ctx, `the object type "%s" do not support the privilege "%s"`, ot.String(), priv.ToString())
		}
	case tree.PRIVILEGE_TYPE_STATIC_SHOW_TABLES:
		privType = PrivilegeTypeShowTables
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_TABLE:
		privType = PrivilegeTypeCreateTable
	case tree.PRIVILEGE_TYPE_STATIC_DROP_TABLE:
		privType = PrivilegeTypeDropTable
	case tree.PRIVILEGE_TYPE_STATIC_CREATE_VIEW:
		privType = PrivilegeTypeCreateView
	case tree.PRIVILEGE_TYPE_STATIC_DROP_VIEW:
		privType = PrivilegeTypeDropView
	case tree.PRIVILEGE_TYPE_STATIC_ALTER_VIEW:
		privType = PrivilegeTypeAlterView
	case tree.PRIVILEGE_TYPE_STATIC_ALTER_TABLE:
		privType = PrivilegeTypeAlterTable
	case tree.PRIVILEGE_TYPE_STATIC_SELECT:
		privType = PrivilegeTypeSelect
	case tree.PRIVILEGE_TYPE_STATIC_INSERT:
		privType = PrivilegeTypeInsert
	case tree.PRIVILEGE_TYPE_STATIC_UPDATE:
		privType = PrivilegeTypeUpdate
	case tree.PRIVILEGE_TYPE_STATIC_DELETE:
		privType = PrivilegeTypeDelete
	case tree.PRIVILEGE_TYPE_STATIC_INDEX:
		privType = PrivilegeTypeIndex
	case tree.PRIVILEGE_TYPE_STATIC_EXECUTE:
		privType = PrivilegeTypeExecute
	case tree.PRIVILEGE_TYPE_STATIC_TRUNCATE:
		privType = PrivilegeTypeTruncate
	case tree.PRIVILEGE_TYPE_STATIC_REFERENCE:
		privType = PrivilegeTypeReference
	case tree.PRIVILEGE_TYPE_STATIC_VALUES:
		privType = PrivilegeTypeValues
	default:
		return 0, moerr.NewInternalError(ctx, "unsupported privilege type %s", priv.ToString())
	}
	return privType, nil
}

// authenticateUserCanExecuteStatementWithObjectTypeNone decides the user has the privilege of executing the statement with object type none
func authenticateUserCanExecuteStatementWithObjectTypeNone(ctx context.Context, ses *Session, stmt tree.Statement) (bool, error) {
	priv := ses.GetPrivilege()
	if priv.objectType() != objectTypeNone { //do nothing
		return true, nil
	}
	tenant := ses.GetTenantInfo()

	if priv.privilegeKind() == privilegeKindNone { // do nothing
		return true, nil
	} else if priv.privilegeKind() == privilegeKindSpecial { //GrantPrivilege, RevokePrivilege

		checkGrantPrivilege := func(g *tree.GrantPrivilege) (bool, error) {
			//in the version 0.6, only the moAdmin and accountAdmin can grant the privilege.
			if tenant.IsAdminRole() {
				return true, nil
			}
			return determineUserCanGrantPrivilegesToOthers(ctx, ses, g)
		}

		checkRevokePrivilege := func() (bool, error) {
			//in the version 0.6, only the moAdmin and accountAdmin can revoke the privilege.
			return tenant.IsAdminRole(), nil
		}

		checkShowAccountsPrivilege := func() (bool, error) {
			//only the moAdmin and accountAdmin can execute the show accounts.
			return tenant.IsAdminRole(), nil
		}

		switch gp := stmt.(type) {
		case *tree.Grant:
			if gp.Typ == tree.GrantTypePrivilege {
				yes, err := checkGrantPrivilege(&gp.GrantPrivilege)
				if err != nil {
					return yes, err
				}
				if yes {
					return yes, nil
				}
			}
		case *tree.Revoke:
			if gp.Typ == tree.RevokeTypePrivilege {
				return checkRevokePrivilege()
			}
		case *tree.GrantPrivilege:
			yes, err := checkGrantPrivilege(gp)
			if err != nil {
				return yes, err
			}
			if yes {
				return yes, nil
			}
		case *tree.RevokePrivilege:
			return checkRevokePrivilege()
		case *tree.ShowAccounts:
			return checkShowAccountsPrivilege()
		}
	}

	return false, nil
}

// checkSysExistsOrNot checks the SYS tenant exists or not.
func checkSysExistsOrNot(ctx context.Context, bh BackgroundExec, pu *config.ParameterUnit) (bool, error) {
	var erArray []ExecResult
	var err error
	var tableNames []string
	var tableName string

	dbSql := "show databases;"
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, dbSql)
	if err != nil {
		return false, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}
	if len(erArray) != 1 {
		return false, moerr.NewInternalError(ctx, "it must have result set")
	}

	for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
		_, err = erArray[0].GetString(ctx, i, 0)
		if err != nil {
			return false, err
		}
	}

	sql := "show tables from mo_catalog;"
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return false, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}
	if len(erArray) != 1 {
		return false, moerr.NewInternalError(ctx, "it must have result set")
	}

	for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
		tableName, err = erArray[0].GetString(ctx, i, 0)
		if err != nil {
			return false, err
		}
		tableNames = append(tableNames, tableName)
	}

	//if there is at least one catalog table, it denotes the sys tenant exists.
	for _, name := range tableNames {
		if _, ok := sysWantedTables[name]; ok {
			return true, nil
		}
	}

	return false, nil
}

// InitSysTenant initializes the tenant SYS before any tenants and accepting any requests
// during the system is booting.
func InitSysTenant(ctx context.Context, aicm *defines.AutoIncrCacheManager) (err error) {
	var exists bool
	var mp *mpool.MPool
	pu := config.GetParameterUnit(ctx)

	tenant := &TenantInfo{
		Tenant:        sysAccountName,
		User:          rootName,
		DefaultRole:   moAdminRoleName,
		TenantID:      sysAccountID,
		UserID:        rootID,
		DefaultRoleID: moAdminRoleID,
	}

	ctx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(sysAccountID))
	ctx = context.WithValue(ctx, defines.UserIDKey{}, uint32(rootID))
	ctx = context.WithValue(ctx, defines.RoleIDKey{}, uint32(moAdminRoleID))

	mp, err = mpool.NewMPool("init_system_tenant", 0, mpool.NoFixed)
	if err != nil {
		return err
	}
	defer mpool.DeleteMPool(mp)
	//Note: it is special here. The connection ctx here is ctx also.
	//Actually, it is ok here. the ctx is moServerCtx instead of requestCtx
	upstream := &Session{
		connectCtx:           ctx,
		autoIncrCacheManager: aicm,
		protocol:             &FakeProtocol{},
		seqCurValues:         make(map[uint64]string),
		seqLastValue:         new(string),
	}
	bh := NewBackgroundHandler(ctx, upstream, mp, pu)
	defer bh.Close()

	//USE the mo_catalog
	err = bh.Exec(ctx, "use mo_catalog;")
	if err != nil {
		return err
	}

	err = bh.Exec(ctx, createDbInformationSchemaSql)
	if err != nil {
		return err
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	exists, err = checkSysExistsOrNot(ctx, bh, pu)
	if err != nil {
		return err
	}

	if !exists {
		err = createTablesInMoCatalog(ctx, bh, tenant, pu)
		if err != nil {
			return err
		}
	}

	return err
}

// createTablesInMoCatalog creates catalog tables in the database mo_catalog.
func createTablesInMoCatalog(ctx context.Context, bh BackgroundExec, tenant *TenantInfo, pu *config.ParameterUnit) error {
	var err error
	var initMoAccount string
	var initDataSqls []string
	if !tenant.IsSysTenant() {
		return moerr.NewInternalError(ctx, "only sys tenant can execute the function")
	}

	addSqlIntoSet := func(sql string) {
		initDataSqls = append(initDataSqls, sql)
	}

	//create tables for the tenant
	for _, sql := range createSqls {
		addSqlIntoSet(sql)
	}

	//initialize the default data of tables for the tenant
	//step 1: add new tenant entry to the mo_account
	initMoAccount = fmt.Sprintf(initMoAccountFormat, sysAccountID, sysAccountName, sysAccountStatus, types.CurrentTimestamp().String2(time.UTC, 0), sysAccountComments)
	addSqlIntoSet(initMoAccount)

	//step 2:add new role entries to the mo_role

	initMoRole1 := fmt.Sprintf(initMoRoleFormat, moAdminRoleID, moAdminRoleName, rootID, moAdminRoleID, types.CurrentTimestamp().String2(time.UTC, 0), "")
	initMoRole2 := fmt.Sprintf(initMoRoleFormat, publicRoleID, publicRoleName, rootID, moAdminRoleID, types.CurrentTimestamp().String2(time.UTC, 0), "")
	addSqlIntoSet(initMoRole1)
	addSqlIntoSet(initMoRole2)

	//step 3:add new user entry to the mo_user

	defaultPassword := rootPassword
	if d := os.Getenv(defaultPasswordEnv); d != "" {
		defaultPassword = d
	}

	//encryption the password
	encryption := HashPassWord(defaultPassword)

	initMoUser1 := fmt.Sprintf(initMoUserFormat, rootID, rootHost, rootName, encryption, rootStatus, types.CurrentTimestamp().String2(time.UTC, 0), rootExpiredTime, rootLoginType, rootCreatorID, rootOwnerRoleID, rootDefaultRoleID)
	initMoUser2 := fmt.Sprintf(initMoUserFormat, dumpID, dumpHost, dumpName, encryption, dumpStatus, types.CurrentTimestamp().String2(time.UTC, 0), dumpExpiredTime, dumpLoginType, dumpCreatorID, dumpOwnerRoleID, dumpDefaultRoleID)
	addSqlIntoSet(initMoUser1)
	addSqlIntoSet(initMoUser2)

	//step4: add new entries to the mo_role_privs
	//moadmin role
	for _, t := range entriesOfMoAdminForMoRolePrivsFor {
		entry := privilegeEntriesMap[t]
		initMoRolePriv := fmt.Sprintf(initMoRolePrivFormat,
			moAdminRoleID, moAdminRoleName,
			entry.objType, entry.objId,
			entry.privilegeId, entry.privilegeId.String(), entry.privilegeLevel,
			rootID, types.CurrentTimestamp().String2(time.UTC, 0),
			entry.withGrantOption)
		addSqlIntoSet(initMoRolePriv)
	}

	//public role
	for _, t := range entriesOfPublicForMoRolePrivsFor {
		entry := privilegeEntriesMap[t]
		initMoRolePriv := fmt.Sprintf(initMoRolePrivFormat,
			publicRoleID, publicRoleName,
			entry.objType, entry.objId,
			entry.privilegeId, entry.privilegeId.String(), entry.privilegeLevel,
			rootID, types.CurrentTimestamp().String2(time.UTC, 0),
			entry.withGrantOption)
		addSqlIntoSet(initMoRolePriv)
	}

	//step5: add new entries to the mo_user_grant

	initMoUserGrant1 := fmt.Sprintf(initMoUserGrantFormat, moAdminRoleID, rootID, types.CurrentTimestamp().String2(time.UTC, 0), false)
	initMoUserGrant2 := fmt.Sprintf(initMoUserGrantFormat, publicRoleID, rootID, types.CurrentTimestamp().String2(time.UTC, 0), false)
	addSqlIntoSet(initMoUserGrant1)
	addSqlIntoSet(initMoUserGrant2)
	initMoUserGrant4 := fmt.Sprintf(initMoUserGrantFormat, moAdminRoleID, dumpID, types.CurrentTimestamp().String2(time.UTC, 0), false)
	initMoUserGrant5 := fmt.Sprintf(initMoUserGrantFormat, publicRoleID, dumpID, types.CurrentTimestamp().String2(time.UTC, 0), false)
	addSqlIntoSet(initMoUserGrant4)
	addSqlIntoSet(initMoUserGrant5)

	//setp6: add new entries to the mo_mysql_compatibility_mode
	for _, variable := range gSysVarsDefs {
		if _, ok := configInitVariables[variable.Name]; ok {
			addsqls := addInitSystemVariablesSql(sysAccountID, sysAccountName, pu)
			for _, sql := range addsqls {
				addSqlIntoSet(sql)
			}
		} else {
			initMoMysqlCompatibilityMode := fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, sysAccountID, sysAccountName, variable.Name, getVariableValue(variable.Default), true)
			addSqlIntoSet(initMoMysqlCompatibilityMode)
		}
	}

	//fill the mo_account, mo_role, mo_user, mo_role_privs, mo_user_grant, mo_mysql_compatibility_mode
	for _, sql := range initDataSqls {
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	}
	return err
}

// createTablesInInformationSchema creates the database information_schema and the views or tables.
func createTablesInInformationSchema(ctx context.Context, bh BackgroundExec, tenant *TenantInfo, pu *config.ParameterUnit) error {
	err := bh.Exec(ctx, "create database if not exists information_schema;")
	if err != nil {
		return err
	}
	return err
}

func checkTenantExistsOrNot(ctx context.Context, bh BackgroundExec, userName string) (bool, error) {
	var sqlForCheckTenant string
	var erArray []ExecResult
	var err error
	ctx, span := trace.Debug(ctx, "checkTenantExistsOrNot")
	defer span.End()
	sqlForCheckTenant, err = getSqlForCheckTenant(ctx, userName)
	if err != nil {
		return false, err
	}
	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sqlForCheckTenant)
	if err != nil {
		return false, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return false, err
	}

	if execResultArrayHasData(erArray) {
		return true, nil
	}
	return false, nil
}

// InitGeneralTenant initializes the application level tenant
func InitGeneralTenant(ctx context.Context, ses *Session, ca *tree.CreateAccount) (err error) {
	var exists bool
	var newTenant *TenantInfo
	var newTenantCtx context.Context
	var mp *mpool.MPool
	var needCreate bool
	ctx, span := trace.Debug(ctx, "InitGeneralTenant")
	defer span.End()
	tenant := ses.GetTenantInfo()

	if !(tenant.IsSysTenant() && tenant.IsMoAdminRole()) {
		return moerr.NewInternalError(ctx, "tenant %s user %s role %s do not have the privilege to create the new account", tenant.GetTenant(), tenant.GetUser(), tenant.GetDefaultRole())
	}

	//normalize the name
	err = normalizeNameOfAccount(ctx, ca)
	if err != nil {
		return err
	}

	ca.AuthOption.AdminName, err = normalizeName(ctx, ca.AuthOption.AdminName)
	if err != nil {
		return err
	}

	if ca.AuthOption.IdentifiedType.Typ == tree.AccountIdentifiedByPassword {
		if len(ca.AuthOption.IdentifiedType.Str) == 0 {
			return moerr.NewInternalError(ctx, "password is empty string")
		}
	}

	ctx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(tenant.GetTenantID()))
	ctx = context.WithValue(ctx, defines.UserIDKey{}, uint32(tenant.GetUserID()))
	ctx = context.WithValue(ctx, defines.RoleIDKey{}, uint32(tenant.GetDefaultRoleID()))

	_, st := trace.Debug(ctx, "InitGeneralTenant.init_general_tenant")
	mp, err = mpool.NewMPool("init_general_tenant", 0, mpool.NoFixed)
	if err != nil {
		st.End()
		return err
	}
	st.End()
	defer mpool.DeleteMPool(mp)

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	//USE the mo_catalog
	err = bh.Exec(ctx, "use mo_catalog;")
	if err != nil {
		return err
	}

	createNewAccount := func() (bool, error) {
		err = bh.Exec(ctx, "begin;")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return false, err
		}

		exists, err = checkTenantExistsOrNot(ctx, bh, ca.Name)
		if err != nil {
			return false, err
		}

		if exists {
			if !ca.IfNotExists { //do nothing
				return false, moerr.NewInternalError(ctx, "the tenant %s exists", ca.Name)
			}
			return false, err
		} else {
			newTenant, newTenantCtx, err = createTablesInMoCatalogOfGeneralTenant(ctx, bh, ca)
			if err != nil {
				return false, err
			}
		}
		return true, err
	}

	needCreate, err = createNewAccount()
	if err != nil {
		return err
	}
	if !needCreate {
		return err
	}

	{
		err = bh.Exec(newTenantCtx, createMoIndexesSql)
		if err != nil {
			return err
		}
		err = bh.Exec(newTenantCtx, createAutoTableSql)
		if err != nil {
			return err
		}

		//create createDbSqls
		createDbSqls := []string{
			"create database " + motrace.SystemDBConst + ";",
			"create database " + mometric.MetricDBConst + ";",
			createDbInformationSchemaSql,
			"create database mysql;",
		}
		for _, db := range createDbSqls {
			err = bh.Exec(newTenantCtx, db)
			if err != nil {
				return err
			}
		}
	}

	createTablesForNewAccount := func() error {
		err = bh.Exec(ctx, "begin;")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return err
		}
		err = createTablesInMoCatalogOfGeneralTenant2(bh, ca, newTenantCtx, newTenant, ses.pu)
		if err != nil {
			return err
		}
		err = createTablesInSystemOfGeneralTenant(ctx, bh, newTenant)
		if err != nil {
			return err
		}
		err = createTablesInInformationSchemaOfGeneralTenant(ctx, bh, newTenant)
		if err != nil {
			return err
		}
		return err
	}

	err = createTablesForNewAccount()
	if err != nil {
		return err
	}

	if !exists {
		//just skip nonexistent pubs
		_ = createSubscriptionDatabase(ctx, bh, newTenant, ses)
	}

	return err
}

// createTablesInMoCatalogOfGeneralTenant creates catalog tables in the database mo_catalog.
func createTablesInMoCatalogOfGeneralTenant(ctx context.Context, bh BackgroundExec, ca *tree.CreateAccount) (*TenantInfo, context.Context, error) {
	var err error
	var initMoAccount string
	var erArray []ExecResult
	var newTenantID int64
	var newUserId int64
	var comment = ""
	var newTenant *TenantInfo
	var newTenantCtx context.Context
	var sql string
	//var configuration string
	//var sql string
	ctx, span := trace.Debug(ctx, "createTablesInMoCatalogOfGeneralTenant")
	defer span.End()

	if nameIsInvalid(ca.Name) {
		return nil, nil, moerr.NewInternalError(ctx, "the account name is invalid")
	}

	if nameIsInvalid(ca.AuthOption.AdminName) {
		return nil, nil, moerr.NewInternalError(ctx, "the admin name is invalid")
	}

	//!!!NOTE : Insert into mo_account with original context.
	// Other operations with a new context with new tenant info
	//step 1: add new tenant entry to the mo_account
	if ca.Comment.Exist {
		comment = ca.Comment.Comment
	}

	//determine the status of the account
	status := sysAccountStatus
	if ca.StatusOption.Exist {
		if ca.StatusOption.Option == tree.AccountStatusSuspend {
			status = tree.AccountStatusSuspend.String()
		}
	}

	initMoAccount = fmt.Sprintf(initMoAccountWithoutIDFormat, ca.Name, status, types.CurrentTimestamp().String2(time.UTC, 0), comment)
	//execute the insert
	err = bh.Exec(ctx, initMoAccount)
	if err != nil {
		return nil, nil, err
	}

	//query the tenant id
	bh.ClearExecResultSet()
	sql, err = getSqlForCheckTenant(ctx, ca.Name)
	if err != nil {
		return nil, nil, err
	}
	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, nil, err
	}

	if execResultArrayHasData(erArray) {
		newTenantID, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, moerr.NewInternalError(ctx, "get the id of tenant %s failed", ca.Name)
	}

	newUserId = dumpID + 1

	newTenant = &TenantInfo{
		Tenant:        ca.Name,
		User:          ca.AuthOption.AdminName,
		DefaultRole:   accountAdminRoleName,
		TenantID:      uint32(newTenantID),
		UserID:        uint32(newUserId),
		DefaultRoleID: accountAdminRoleID,
	}
	//with new tenant
	newTenantCtx = context.WithValue(ctx, defines.TenantIDKey{}, uint32(newTenantID))
	newTenantCtx = context.WithValue(newTenantCtx, defines.UserIDKey{}, uint32(newUserId))
	newTenantCtx = context.WithValue(newTenantCtx, defines.RoleIDKey{}, uint32(accountAdminRoleID))
	return newTenant, newTenantCtx, err
}

func createTablesInMoCatalogOfGeneralTenant2(bh BackgroundExec, ca *tree.CreateAccount, newTenantCtx context.Context, newTenant *TenantInfo, pu *config.ParameterUnit) error {
	var err error
	var initDataSqls []string
	newTenantCtx, span := trace.Debug(newTenantCtx, "createTablesInMoCatalogOfGeneralTenant2")
	defer span.End()
	//create tables for the tenant
	for _, sql := range createSqls {
		//only the SYS tenant has the table mo_account
		if strings.HasPrefix(sql, "create table mo_account") {
			continue
		}
		err = bh.Exec(newTenantCtx, sql)
		if err != nil {
			return err
		}
	}

	//initialize the default data of tables for the tenant
	addSqlIntoSet := func(sql string) {
		initDataSqls = append(initDataSqls, sql)
	}
	//step 2:add new role entries to the mo_role
	initMoRole1 := fmt.Sprintf(initMoRoleFormat, accountAdminRoleID, accountAdminRoleName, newTenant.GetUserID(), newTenant.GetDefaultRoleID(), types.CurrentTimestamp().String2(time.UTC, 0), "")
	initMoRole2 := fmt.Sprintf(initMoRoleFormat, publicRoleID, publicRoleName, newTenant.GetUserID(), newTenant.GetDefaultRoleID(), types.CurrentTimestamp().String2(time.UTC, 0), "")
	addSqlIntoSet(initMoRole1)
	addSqlIntoSet(initMoRole2)

	//step 3:add new user entry to the mo_user
	if ca.AuthOption.IdentifiedType.Typ != tree.AccountIdentifiedByPassword {
		err = moerr.NewInternalError(newTenantCtx, "only support password verification now")
		return err
	}
	name := ca.AuthOption.AdminName
	password := ca.AuthOption.IdentifiedType.Str
	if len(password) == 0 {
		err = moerr.NewInternalError(newTenantCtx, "password is empty string")
		return err
	}
	//encryption the password
	encryption := HashPassWord(password)
	status := rootStatus
	//TODO: fix the status of user or account
	if ca.StatusOption.Exist {
		if ca.StatusOption.Option == tree.AccountStatusSuspend {
			status = tree.AccountStatusSuspend.String()
		}
	}
	//the first user id in the general tenant
	initMoUser1 := fmt.Sprintf(initMoUserFormat, newTenant.GetUserID(), rootHost, name, encryption, status,
		types.CurrentTimestamp().String2(time.UTC, 0), rootExpiredTime, rootLoginType,
		newTenant.GetUserID(), newTenant.GetDefaultRoleID(), accountAdminRoleID)
	addSqlIntoSet(initMoUser1)

	//step4: add new entries to the mo_role_privs
	//accountadmin role
	for _, t := range entriesOfAccountAdminForMoRolePrivsFor {
		entry := privilegeEntriesMap[t]
		initMoRolePriv := fmt.Sprintf(initMoRolePrivFormat,
			accountAdminRoleID, accountAdminRoleName,
			entry.objType, entry.objId,
			entry.privilegeId, entry.privilegeId.String(), entry.privilegeLevel,
			newTenant.GetUserID(), types.CurrentTimestamp().String2(time.UTC, 0),
			entry.withGrantOption)
		addSqlIntoSet(initMoRolePriv)
	}

	//public role
	for _, t := range entriesOfPublicForMoRolePrivsFor {
		entry := privilegeEntriesMap[t]
		initMoRolePriv := fmt.Sprintf(initMoRolePrivFormat,
			publicRoleID, publicRoleName,
			entry.objType, entry.objId,
			entry.privilegeId, entry.privilegeId.String(), entry.privilegeLevel,
			newTenant.GetUserID(), types.CurrentTimestamp().String2(time.UTC, 0),
			entry.withGrantOption)
		addSqlIntoSet(initMoRolePriv)
	}

	//step5: add new entries to the mo_user_grant
	initMoUserGrant1 := fmt.Sprintf(initMoUserGrantFormat, accountAdminRoleID, newTenant.GetUserID(), types.CurrentTimestamp().String2(time.UTC, 0), true)
	addSqlIntoSet(initMoUserGrant1)
	initMoUserGrant2 := fmt.Sprintf(initMoUserGrantFormat, publicRoleID, newTenant.GetUserID(), types.CurrentTimestamp().String2(time.UTC, 0), true)
	addSqlIntoSet(initMoUserGrant2)

	//setp6: add new entries to the mo_mysql_compatibility_mode
	for _, variable := range gSysVarsDefs {
		if _, ok := configInitVariables[variable.Name]; ok {
			addsqls := addInitSystemVariablesSql(int(newTenant.GetTenantID()), newTenant.GetTenant(), pu)
			for _, sql := range addsqls {
				addSqlIntoSet(sql)
			}
		} else {
			initMoMysqlCompatibilityMode := fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, newTenant.GetTenantID(), newTenant.GetTenant(), variable.Name, getVariableValue(variable.Default), true)
			addSqlIntoSet(initMoMysqlCompatibilityMode)
		}
	}

	//fill the mo_role, mo_user, mo_role_privs, mo_user_grant, mo_role_grant
	for _, sql := range initDataSqls {
		bh.ClearExecResultSet()
		err = bh.Exec(newTenantCtx, sql)
		if err != nil {
			return err
		}
	}
	return nil
}

// createTablesInSystemOfGeneralTenant creates the database system and system_metrics as the external tables.
func createTablesInSystemOfGeneralTenant(ctx context.Context, bh BackgroundExec, newTenant *TenantInfo) error {
	ctx, span := trace.Debug(ctx, "createTablesInSystemOfGeneralTenant")
	defer span.End()
	//with new tenant
	ctx = context.WithValue(ctx, defines.TenantIDKey{}, newTenant.GetTenantID())
	ctx = context.WithValue(ctx, defines.UserIDKey{}, newTenant.GetUserID())
	ctx = context.WithValue(ctx, defines.RoleIDKey{}, newTenant.GetDefaultRoleID())

	var err error
	sqls := make([]string, 0)
	sqls = append(sqls, "use "+motrace.SystemDBConst+";")
	traceTables := motrace.GetSchemaForAccount(ctx, newTenant.GetTenant())
	sqls = append(sqls, traceTables...)
	sqls = append(sqls, "use "+mometric.MetricDBConst+";")
	metricTables := mometric.GetSchemaForAccount(ctx, newTenant.GetTenant())
	sqls = append(sqls, metricTables...)

	for _, sql := range sqls {
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	}
	return err
}

// createTablesInInformationSchemaOfGeneralTenant creates the database information_schema and the views or tables.
func createTablesInInformationSchemaOfGeneralTenant(ctx context.Context, bh BackgroundExec, newTenant *TenantInfo) error {
	ctx, span := trace.Debug(ctx, "createTablesInInformationSchemaOfGeneralTenant")
	defer span.End()
	//with new tenant
	//TODO: when we have the auto_increment column, we need new strategy.
	ctx = context.WithValue(ctx, defines.TenantIDKey{}, newTenant.GetTenantID())
	ctx = context.WithValue(ctx, defines.UserIDKey{}, newTenant.GetUserID())
	ctx = context.WithValue(ctx, defines.RoleIDKey{}, newTenant.GetDefaultRoleID())

	var err error
	sqls := make([]string, 0, len(sysview.InitInformationSchemaSysTables)+len(sysview.InitMysqlSysTables)+4)

	sqls = append(sqls, "use information_schema;")
	sqls = append(sqls, sysview.InitInformationSchemaSysTables...)
	sqls = append(sqls, "use mysql;")
	sqls = append(sqls, sysview.InitMysqlSysTables...)

	for _, sql := range sqls {
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	}
	return err
}

// create subscription database
func createSubscriptionDatabase(ctx context.Context, bh BackgroundExec, newTenant *TenantInfo, ses *Session) error {
	ctx, span := trace.Debug(ctx, "createSubscriptionDatabase")
	defer span.End()

	var err error
	subscriptions := make([]string, 0)
	//process the syspublications
	_, syspublications_value, _ := ses.GetGlobalSysVars().GetGlobalSysVar("syspublications")
	if syspublications, ok := syspublications_value.(string); ok {
		if len(syspublications) == 0 {
			return err
		}
		subscriptions = strings.Split(syspublications, ",")
	}
	// if no subscriptions, return
	if len(subscriptions) == 0 {
		return err
	}

	//with new tenant
	ctx = context.WithValue(ctx, defines.TenantIDKey{}, newTenant.GetTenantID())
	ctx = context.WithValue(ctx, defines.UserIDKey{}, newTenant.GetUserID())
	ctx = context.WithValue(ctx, defines.RoleIDKey{}, newTenant.GetDefaultRoleID())

	createSubscriptionFormat := `create database %s from sys publication %s;`
	sqls := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		sqls = append(sqls, fmt.Sprintf(createSubscriptionFormat, subscription, subscription))
	}
	for _, sql := range sqls {
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
	}
	return err
}

// InitUser creates new user for the tenant
func InitUser(ctx context.Context, ses *Session, tenant *TenantInfo, cu *tree.CreateUser) (err error) {
	var exists int
	var erArray []ExecResult
	var newUserId int64
	var host string
	var newRoleId int64
	var status string
	var sql string
	var mp *mpool.MPool

	err = normalizeNamesOfUsers(ctx, cu.Users)
	if err != nil {
		return err
	}

	if cu.Role != nil {
		err = normalizeNameOfRole(ctx, cu.Role)
		if err != nil {
			return err
		}
	}

	mp, err = mpool.NewMPool("init_user", 0, mpool.NoFixed)
	if err != nil {
		return err
	}
	defer mpool.DeleteMPool(mp)

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	//TODO: get role and the id of role
	newRoleId = publicRoleID
	if cu.Role != nil {
		sql, err = getSqlForRoleIdOfRole(ctx, cu.Role.UserName)
		if err != nil {
			return err
		}
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}
		if !execResultArrayHasData(erArray) {
			return moerr.NewInternalError(ctx, "there is no role %s", cu.Role.UserName)
		}
		newRoleId, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return err
		}

		from := &verifiedRole{
			typ:  roleType,
			name: cu.Role.UserName,
		}

		for _, user := range cu.Users {
			to := &verifiedRole{
				typ:  userType,
				name: user.Username,
			}
			err = verifySpecialRolesInGrant(ctx, tenant, from, to)
			if err != nil {
				return err
			}
		}
	}

	//TODO: get password_option or lock_option. there is no field in mo_user to store it.
	status = userStatusUnlock
	if cu.MiscOpt != nil {
		if _, ok := cu.MiscOpt.(*tree.UserMiscOptionAccountLock); ok {
			status = userStatusLock
		}
	}

	for _, user := range cu.Users {
		//dedup with user
		sql, err = getSqlForPasswordOfUser(ctx, user.Username)
		if err != nil {
			return err
		}
		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}
		exists = 0
		if execResultArrayHasData(erArray) {
			exists = 1
		}

		//dedup with the role
		if exists == 0 {
			sql, err = getSqlForRoleIdOfRole(ctx, user.Username)
			if err != nil {
				return err
			}
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}
			if execResultArrayHasData(erArray) {
				exists = 2
			}
		}

		if exists != 0 {
			if cu.IfNotExists { //do nothing
				continue
			}
			if exists == 1 {
				err = moerr.NewInternalError(ctx, "the user %s exists", user.Username)
			} else if exists == 2 {
				err = moerr.NewInternalError(ctx, "there is a role with the same name as the user")
			}

			return err
		}

		if user.AuthOption == nil {
			return moerr.NewInternalError(ctx, "the user %s misses the auth_option", user.Username)
		}

		if user.AuthOption.Typ != tree.AccountIdentifiedByPassword {
			return moerr.NewInternalError(ctx, "only support password verification now")
		}

		password := user.AuthOption.Str
		if len(password) == 0 {
			return moerr.NewInternalError(ctx, "password is empty string")
		}

		//encryption the password
		encryption := HashPassWord(password)

		//TODO: get comment or attribute. there is no field in mo_user to store it.
		host = user.Hostname
		if len(user.Hostname) == 0 || user.Hostname == "%" {
			host = rootHost
		}
		initMoUser1 := fmt.Sprintf(initMoUserWithoutIDFormat, host, user.Username, encryption, status,
			types.CurrentTimestamp().String2(time.UTC, 0), rootExpiredTime, rootLoginType,
			tenant.GetUserID(), tenant.GetDefaultRoleID(), newRoleId)

		bh.ClearExecResultSet()
		err = bh.Exec(ctx, initMoUser1)
		if err != nil {
			return err
		}

		//query the id
		bh.ClearExecResultSet()
		sql, err = getSqlForPasswordOfUser(ctx, user.Username)
		if err != nil {
			return err
		}
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if !execResultArrayHasData(erArray) {
			return moerr.NewInternalError(ctx, "get the id of user %s failed", user.Username)
		}
		newUserId, err = erArray[0].GetInt64(ctx, 0, 0)
		if err != nil {
			return err
		}

		initMoUserGrant1 := fmt.Sprintf(initMoUserGrantFormat, newRoleId, newUserId, types.CurrentTimestamp().String2(time.UTC, 0), true)
		err = bh.Exec(ctx, initMoUserGrant1)
		if err != nil {
			return err
		}

		//if it is not public role, just insert the record for public
		if newRoleId != publicRoleID {
			initMoUserGrant2 := fmt.Sprintf(initMoUserGrantFormat, publicRoleID, newUserId, types.CurrentTimestamp().String2(time.UTC, 0), true)
			err = bh.Exec(ctx, initMoUserGrant2)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// InitRole creates the new role
func InitRole(ctx context.Context, ses *Session, tenant *TenantInfo, cr *tree.CreateRole) (err error) {
	var exists int
	var erArray []ExecResult
	var sql string
	err = normalizeNamesOfRoles(ctx, cr.Roles)
	if err != nil {
		return err
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	for _, r := range cr.Roles {
		exists = 0
		if isPredefinedRole(r.UserName) {
			exists = 3
		} else {
			//dedup with role
			sql, err = getSqlForRoleIdOfRole(ctx, r.UserName)
			if err != nil {
				return err
			}
			bh.ClearExecResultSet()
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}

			erArray, err = getResultSet(ctx, bh)
			if err != nil {
				return err
			}
			if execResultArrayHasData(erArray) {
				exists = 1
			}

			//dedup with user
			if exists == 0 {
				sql, err = getSqlForPasswordOfUser(ctx, r.UserName)
				if err != nil {
					return err
				}
				bh.ClearExecResultSet()
				err = bh.Exec(ctx, sql)
				if err != nil {
					return err
				}

				erArray, err = getResultSet(ctx, bh)
				if err != nil {
					return err
				}
				if execResultArrayHasData(erArray) {
					exists = 2
				}
			}
		}

		if exists != 0 {
			if cr.IfNotExists {
				continue
			}
			if exists == 1 {
				err = moerr.NewInternalError(ctx, "the role %s exists", r.UserName)
			} else if exists == 2 {
				err = moerr.NewInternalError(ctx, "there is a user with the same name as the role %s", r.UserName)
			} else if exists == 3 {
				err = moerr.NewInternalError(ctx, "can not use the name %s. it is the name of the predefined role", r.UserName)
			}

			return err
		}

		initMoRole := fmt.Sprintf(initMoRoleWithoutIDFormat, r.UserName, tenant.GetUserID(), tenant.GetDefaultRoleID(),
			types.CurrentTimestamp().String2(time.UTC, 0), "")
		err = bh.Exec(ctx, initMoRole)
		if err != nil {
			return err
		}
	}
	return err
}

func InitFunction(ctx context.Context, ses *Session, tenant *TenantInfo, cf *tree.CreateFunction) (err error) {
	var initMoUdf string
	var retTypeStr string
	var dbName string
	var checkExistence string
	var argsJson []byte
	var fmtctx *tree.FmtCtx
	var argMap map[string]string
	var erArray []ExecResult

	// a database must be selected or specified as qualifier when create a function
	if cf.Name.HasNoNameQualifier() {
		if ses.DatabaseNameIsEmpty() {
			return moerr.NewNoDBNoCtx()
		}
		dbName = ses.GetDatabaseName()
	} else {
		dbName = string(cf.Name.Name.SchemaName)
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	// format return type
	fmtctx = tree.NewFmtCtx(dialect.MYSQL, tree.WithQuoteString(true))
	cf.ReturnType.Format(fmtctx)
	retTypeStr = fmtctx.String()
	fmtctx.Reset()

	// build argmap and marshal as json
	argMap = make(map[string]string)
	for i := 0; i < len(cf.Args); i++ {
		curName := cf.Args[i].GetName(fmtctx)
		fmtctx.Reset()
		argMap[curName] = cf.Args[i].GetType(fmtctx)
		fmtctx.Reset()
	}
	argsJson, err = json.Marshal(argMap)
	if err != nil {
		return err
	}

	// validate duplicate function declaration
	bh.ClearExecResultSet()
	checkExistence = fmt.Sprintf(checkUdfExistence, string(cf.Name.Name.ObjectName), dbName, string(argsJson))
	err = bh.Exec(ctx, checkExistence)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}

	if execResultArrayHasData(erArray) {
		return moerr.NewUDFAlreadyExistsNoCtx(string(cf.Name.Name.ObjectName))
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	initMoUdf = fmt.Sprintf(initMoUserDefinedFunctionFormat,
		string(cf.Name.Name.ObjectName),
		ses.GetTenantInfo().GetDefaultRoleID(),
		string(argsJson),
		retTypeStr, cf.Body, cf.Language, dbName,
		tenant.User, types.CurrentTimestamp().String2(time.UTC, 0), types.CurrentTimestamp().String2(time.UTC, 0), "FUNCTION", "DEFINER", "", "utf8mb4", "utf8mb4_0900_ai_ci", "utf8mb4_0900_ai_ci")
	err = bh.Exec(ctx, initMoUdf)
	if err != nil {
		return err
	}

	return err
}

func InitProcedure(ctx context.Context, ses *Session, tenant *TenantInfo, cp *tree.CreateProcedure) (err error) {
	var initMoProcedure string
	var dbName string
	var checkExistence string
	var argsJson []byte
	// var fmtctx *tree.FmtCtx
	var erArray []ExecResult

	// a database must be selected or specified as qualifier when create a function
	if cp.Name.HasNoNameQualifier() {
		if ses.DatabaseNameIsEmpty() {
			return moerr.NewNoDBNoCtx()
		}
		dbName = ses.GetDatabaseName()
	} else {
		dbName = string(cp.Name.Name.SchemaName)
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	// build argmap and marshal as json
	fmtctx := tree.NewFmtCtx(dialect.MYSQL, tree.WithQuoteString(true))

	// build argmap and marshal as json
	argList := make(map[string]tree.ProcedureArgForMarshal)
	for i := 0; i < len(cp.Args); i++ {
		curName := cp.Args[i].GetName(fmtctx)
		fmtctx.Reset()
		argList[curName] = tree.ProcedureArgForMarshal{
			Name:      cp.Args[i].(*tree.ProcedureArgDecl).Name,
			Type:      cp.Args[i].(*tree.ProcedureArgDecl).Type,
			InOutType: cp.Args[i].(*tree.ProcedureArgDecl).InOutType,
		}
	}
	argsJson, err = json.Marshal(argList)
	if err != nil {
		return err
	}

	// validate duplicate procedure declaration
	bh.ClearExecResultSet()
	checkExistence = getSqlForCheckProcedureExistence(string(cp.Name.Name.ObjectName), dbName)
	err = bh.Exec(ctx, checkExistence)
	if err != nil {
		return err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return err
	}

	if execResultArrayHasData(erArray) {
		return moerr.NewProcedureAlreadyExistsNoCtx(string(cp.Name.Name.ObjectName))
	}

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return err
	}

	initMoProcedure = fmt.Sprintf(initMoStoredProcedureFormat,
		string(cp.Name.Name.ObjectName),
		string(argsJson),
		cp.Body, dbName,
		tenant.User, types.CurrentTimestamp().String2(time.UTC, 0), types.CurrentTimestamp().String2(time.UTC, 0), "PROCEDURE", "DEFINER", "", "utf8mb4", "utf8mb4_0900_ai_ci", "utf8mb4_0900_ai_ci")
	err = bh.Exec(ctx, initMoProcedure)
	if err != nil {
		return err
	}
	return err
}

func doAlterDatabaseConfig(ctx context.Context, ses *Session, ad *tree.AlterDataBaseConfig) (err error) {
	var sql string
	var erArray []ExecResult
	var accountName string

	dbName := ad.DbName
	updateConfig := ad.UpdateConfig
	accountName = ses.GetTenantInfo().GetTenant()

	updateConfigForDatabase := func() error {
		bh := ses.GetBackgroundExec(ctx)
		defer bh.Close()

		err = bh.Exec(ctx, "begin")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return err
		}

		// step1:check database exists or not
		sql, err = getSqlForCheckDatabase(ctx, dbName)
		if err != nil {
			return err
		}

		bh.ClearExecResultSet()
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}

		erArray, err = getResultSet(ctx, bh)
		if err != nil {
			return err
		}

		if !execResultArrayHasData(erArray) {
			return moerr.NewInternalError(ctx, "there is no database %s to change config", dbName)
		}

		// step2: update the mo_mysql_compatibility_mode of that database
		sql, err = getSqlForupdateConfigurationByDbNameAndAccountName(ctx, updateConfig, accountName, dbName, "version_compatibility")
		if err != nil {
			return err
		}

		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
		return err
	}

	err = updateConfigForDatabase()
	if err != nil {
		return err
	}

	// step3: update the session verison
	if len(ses.GetDatabaseName()) != 0 && ses.GetDatabaseName() == dbName {
		err = changeVersion(ctx, ses, ses.GetDatabaseName())
		if err != nil {
			return err
		}
	}

	return err
}

func doAlterAccountConfig(ctx context.Context, ses *Session, stmt *tree.AlterDataBaseConfig) (err error) {
	var sql string
	var newCtx context.Context
	var isExist bool

	accountName := stmt.AccountName
	update_config := stmt.UpdateConfig

	updateConfigForAccount := func() error {
		bh := ses.GetBackgroundExec(ctx)
		defer bh.Close()

		err = bh.Exec(ctx, "begin")
		defer func() {
			err = finishTxn(ctx, bh, err)
		}()
		if err != nil {
			return err
		}

		// step 1: check account exists or not
		newCtx = context.WithValue(ctx, defines.TenantIDKey{}, catalog.System_Account)
		isExist, err = checkTenantExistsOrNot(newCtx, bh, accountName)
		if err != nil {
			return err
		}

		if !isExist {
			return moerr.NewInternalError(ctx, "there is no account %s to change config", accountName)
		}

		// step2: update the config
		sql, err = getSqlForupdateConfigurationByAccount(ctx, update_config, accountName, "version_compatibility")
		if err != nil {
			return err
		}
		err = bh.Exec(ctx, sql)
		if err != nil {
			return err
		}
		return err
	}

	err = updateConfigForAccount()
	if err != nil {
		return err
	}

	// step3: update the session verison
	if len(ses.GetDatabaseName()) != 0 {
		err = changeVersion(ctx, ses, ses.GetDatabaseName())
		if err != nil {
			return err
		}
	}

	return err
}

func insertRecordToMoMysqlCompatibilityMode(ctx context.Context, ses *Session, stmt tree.Statement) (err error) {
	var sql string
	var accountId uint32
	var accountName string
	var dbName string
	variableName := "version_compatibility"
	variableValue := "0.7"

	if createDatabaseStmt, ok := stmt.(*tree.CreateDatabase); ok {
		dbName = string(createDatabaseStmt.Name)
		//if create sys database, do nothing
		if _, ok = sysDatabases[dbName]; ok {
			return nil
		}

		insertRecordFunc := func() error {
			bh := ses.GetBackgroundExec(ctx)
			defer bh.Close()

			err = bh.Exec(ctx, "begin")
			defer func() {
				err = finishTxn(ctx, bh, err)
			}()
			if err != nil {
				return err
			}

			//step 1: get account_name and database_name
			if ses.GetTenantInfo() != nil {
				accountName = ses.GetTenantInfo().GetTenant()
				accountId = ses.GetTenantInfo().GetTenantID()
			} else {
				return err
			}

			//step 2: check database name
			if _, ok = bannedCatalogDatabases[dbName]; ok {
				return nil
			}

			//step 3: insert the record
			sql = fmt.Sprintf(initMoMysqlCompatbilityModeFormat, accountId, accountName, dbName, variableName, variableValue, false)

			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
			return err
		}
		err = insertRecordFunc()
		if err != nil {
			return err
		}
	}
	return nil

}

func deleteRecordToMoMysqlCompatbilityMode(ctx context.Context, ses *Session, stmt tree.Statement) (err error) {
	var datname string
	var sql string

	if deleteDatabaseStmt, ok := stmt.(*tree.DropDatabase); ok {
		datname = string(deleteDatabaseStmt.Name)
		//if delete sys database, do nothing
		if _, ok = sysDatabases[datname]; ok {
			return nil
		}

		deleteRecordFunc := func() error {
			bh := ses.GetBackgroundExec(ctx)
			defer bh.Close()

			err = bh.Exec(ctx, "begin")
			defer func() {
				err = finishTxn(ctx, bh, err)
			}()
			if err != nil {
				return err
			}
			sql = getSqlForDeleteMysqlCompatbilityMode(datname)

			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
			return err
		}
		err = deleteRecordFunc()
		if err != nil {
			return err
		}
	}
	return nil
}

func GetVersionCompatibility(ctx context.Context, ses *Session, dbName string) (ret string, err error) {
	var erArray []ExecResult
	var sql string
	var resultConfig string
	defaultConfig := "0.7"
	variableName := "version_compatibility"
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	err = bh.Exec(ctx, "begin")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return defaultConfig, err
	}

	sql = getSqlForGetSystemVariableValueWithDatabase(dbName, variableName)

	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return defaultConfig, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return defaultConfig, err
	}

	if execResultArrayHasData(erArray) {
		resultConfig, err = erArray[0].GetString(ctx, 0, 0)
		if err != nil {
			return defaultConfig, err
		}
	}

	return resultConfig, err
}

func doInterpretCall(ctx context.Context, ses *Session, call *tree.CallStmt) ([]ExecResult, error) {
	// fetch related
	var spBody string
	var dbName string
	var sql string
	var argstr string
	var err error
	var erArray []ExecResult
	var argList map[string]tree.ProcedureArgForMarshal
	// execute related
	var interpreter Interpreter
	var varScope [](map[string]interface{})
	var argsMap map[string]tree.Expr
	var argsAttr map[string]tree.InOutArgType

	// a database must be selected or specified as qualifier when create a function
	if call.Name.HasNoNameQualifier() {
		if ses.DatabaseNameIsEmpty() {
			return nil, moerr.NewNoDBNoCtx()
		}
		dbName = ses.GetDatabaseName()
	} else {
		dbName = string(call.Name.Name.SchemaName)
	}

	sql, err = getSqlForSpBody(ctx, string(call.Name.Name.ObjectName), dbName)
	if err != nil {
		return nil, err
	}

	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	bh.ClearExecResultSet()

	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, err
	}

	if execResultArrayHasData(erArray) {
		// function with provided name and db exists, for now we don't support overloading for stored procedure, so go to handle deletion.
		spBody, err = erArray[0].GetString(ctx, 0, 0)
		if err != nil {
			return nil, err
		}
		argstr, err = erArray[0].GetString(ctx, 0, 1)
		if err != nil {
			return nil, err
		}

		// perform argument length validation
		// postpone argument type check until actual execution of its procedure body. This will be handled by the binder.
		err = json.Unmarshal([]byte(argstr), &argList)
		if err != nil {
			return nil, err
		}
		if len(argList) != len(call.Args) {
			return nil, moerr.NewInvalidArg(ctx, string(call.Name.Name.ObjectName)+" procedure have invalid input args length", len(call.Args))
		}
	} else {
		return nil, moerr.NewNoUDFNoCtx(string(call.Name.Name.ObjectName))
	}

	stmt, err := parsers.Parse(ctx, dialect.MYSQL, spBody, 1)
	if err != nil {
		return nil, err
	}

	fmtctx := tree.NewFmtCtx(dialect.MYSQL, tree.WithQuoteString(true))

	argsAttr = make(map[string]tree.InOutArgType)
	argsMap = make(map[string]tree.Expr) // map arg to param

	// build argsAttr and argsMap
	logutil.Info("Interpret procedure call length:" + strconv.Itoa(len(argList)))
	i := 0
	for curName, v := range argList {
		argsAttr[curName] = v.InOutType
		argsMap[curName] = call.Args[i]
		i++
	}

	interpreter.ctx = ctx
	interpreter.fmtctx = fmtctx
	interpreter.ses = ses
	interpreter.varScope = &varScope
	interpreter.bh = bh
	interpreter.result = nil
	interpreter.argsMap = argsMap
	interpreter.argsAttr = argsAttr
	interpreter.outParamMap = make(map[string]interface{})

	err = interpreter.ExecuteSp(stmt[0], dbName)
	if err != nil {
		return nil, err
	}
	return interpreter.GetResult(), nil
}

func doGrantPrivilegeImplicitly(ctx context.Context, ses *Session, stmt tree.Statement) error {
	var err error
	var sql string
	tenantInfo := ses.GetTenantInfo()
	if tenantInfo == nil || tenantInfo.IsAdminRole() {
		return err
	}
	currentRole := tenantInfo.GetDefaultRole()

	// 1.first change to moadmin/accountAdmin
	var tenantCtx context.Context
	tenantInfo = ses.GetTenantInfo()
	// if is system account
	if tenantInfo.IsSysTenant() {
		tenantCtx = context.WithValue(ses.GetRequestContext(), defines.TenantIDKey{}, uint32(sysAccountID))
		tenantCtx = context.WithValue(tenantCtx, defines.UserIDKey{}, uint32(rootID))
		tenantCtx = context.WithValue(tenantCtx, defines.RoleIDKey{}, uint32(moAdminRoleID))
	} else {
		tenantCtx = context.WithValue(ses.GetRequestContext(), defines.TenantIDKey{}, tenantInfo.GetTenantID())
		tenantCtx = context.WithValue(tenantCtx, defines.UserIDKey{}, tenantInfo.GetUserID())
		tenantCtx = context.WithValue(tenantCtx, defines.RoleIDKey{}, uint32(accountAdminRoleID))
	}

	// 2.grant database privilege
	switch st := stmt.(type) {
	case *tree.CreateDatabase:
		sql = getSqlForGrantOwnershipOnDatabase(string(st.Name), currentRole)
	case *tree.CreateTable:
		// get database name
		var dbName string
		if len(st.Table.SchemaName) == 0 {
			dbName = ses.GetDatabaseName()
		} else {
			dbName = string(st.Table.SchemaName)
		}
		// get table name
		tableName := string(st.Table.ObjectName)
		sql = getSqlForGrantOwnershipOnTable(dbName, tableName, currentRole)
	}

	bh := ses.GetBackgroundExec(tenantCtx)
	defer bh.Close()

	err = bh.Exec(tenantCtx, sql)
	if err != nil {
		return err
	}

	return err
}

func doGetGlobalSystemVariable(ctx context.Context, ses *Session) (ret map[string]interface{}, err error) {
	var sql string
	var erArray []ExecResult
	var sysVars map[string]interface{}
	var accountId uint32
	var variableName, variableValue string
	var val interface{}
	tenantInfo := ses.GetTenantInfo()

	sysVars = make(map[string]interface{})
	bh := ses.GetBackgroundExec(ctx)
	defer bh.Close()

	err = bh.Exec(ctx, "begin;")
	defer func() {
		err = finishTxn(ctx, bh, err)
	}()
	if err != nil {
		return nil, err
	}

	accountId = tenantInfo.GetTenantID()
	sql = getSystemVariablesWithAccount(uint64(accountId))

	bh.ClearExecResultSet()
	err = bh.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}

	erArray, err = getResultSet(ctx, bh)
	if err != nil {
		return nil, err
	}

	if execResultArrayHasData(erArray) {
		for i := uint64(0); i < erArray[0].GetRowCount(); i++ {
			variableName, err = erArray[0].GetString(ctx, i, 0)
			if err != nil {
				return nil, err
			}
			variableValue, err = erArray[0].GetString(ctx, i, 1)
			if err != nil {
				return nil, err
			}

			if sv, ok := gSysVarsDefs[variableName]; ok {
				val, err = sv.GetType().ConvertFromString(variableValue)
				if err != nil {
					return nil, err
				}
				sysVars[variableName] = val
			}
		}
	}

	return sysVars, nil
}

func doSetGlobalSystemVariable(ctx context.Context, ses *Session, varName string, varValue interface{}) (err error) {
	var sql string
	var accountId uint32
	tenantInfo := ses.GetTenantInfo()

	varName = strings.ToLower(varName)
	if sv, ok := gSysVarsDefs[varName]; ok {
		if sv.GetScope() == ScopeSession {
			return moerr.NewInternalError(ctx, errorSystemVariableIsSession())
		}
		if !sv.GetDynamic() {
			return moerr.NewInternalError(ctx, errorSystemVariableIsReadOnly())
		}

		setGlobalFunc := func() error {
			bh := ses.GetBackgroundExec(ctx)
			defer bh.Close()

			err = bh.Exec(ctx, "begin;")
			defer func() {
				err = finishTxn(ctx, bh, err)
			}()
			if err != nil {
				return err
			}

			accountId = tenantInfo.GetTenantID()
			sql = getSqlForUpdateSystemVariableValue(getVariableValue(varValue), uint64(accountId), varName)
			err = bh.Exec(ctx, sql)
			if err != nil {
				return err
			}
			return err
		}
		err = setGlobalFunc()
		if err != nil {
			return err
		}
		return err
	} else {
		return moerr.NewInternalError(ctx, errorSystemVariableDoesNotExist())
	}
}

func doCheckRole(ctx context.Context, ses *Session) error {
	var err error
	tenantInfo := ses.GetTenantInfo()
	currentAccount := tenantInfo.GetTenant()
	currentRole := tenantInfo.GetDefaultRole()
	if currentAccount == sysAccountName {
		if currentRole != moAdminRoleName {
			err = moerr.NewInternalError(ctx, "do not have privilege to execute the statement")
		}
	} else {
		if currentRole != accountAdminRoleName {
			err = moerr.NewInternalError(ctx, "do not have privilege to execute the statement")
		}
	}
	return err
}

// isSuperUser returns true if the username is dump or root.
func isSuperUser(username string) bool {
	u := strings.ToLower(username)
	return u == dumpName || u == rootName
}

func addInitSystemVariablesSql(accountId int, accountName string, pu *config.ParameterUnit) []string {
	returnSql := make([]string, 0)
	var initMoMysqlCompatibilityMode string

	if strings.ToLower(pu.SV.SaveQueryResult) == "on" {
		initMoMysqlCompatibilityMode = fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, accountId, accountName, "save_query_result", pu.SV.SaveQueryResult, true)
		returnSql = append(returnSql, initMoMysqlCompatibilityMode)
	}

	initMoMysqlCompatibilityMode = fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, accountId, accountName, "query_result_maxsize", pu.SV.QueryResultMaxsize, true)
	returnSql = append(returnSql, initMoMysqlCompatibilityMode)

	initMoMysqlCompatibilityMode = fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, accountId, accountName, "query_result_timeout", pu.SV.QueryResultTimeout, true)
	returnSql = append(returnSql, initMoMysqlCompatibilityMode)

	initMoMysqlCompatibilityMode = fmt.Sprintf(initMoMysqlCompatbilityModeWithoutDataBaseFormat, accountId, accountName, "lower_case_table_names", pu.SV.LowerCaseTableNames, true)
	returnSql = append(returnSql, initMoMysqlCompatibilityMode)

	return returnSql
}
