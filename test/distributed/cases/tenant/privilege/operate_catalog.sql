set global enable_privilege_cache = off;
-- the administrator can only read the content of catalog.
drop database mo_catalog;
drop database system;
drop database system_metrics;
drop database information_schema;

use mo_catalog;
drop table mo_database;
drop table mo_tables;
drop table mo_columns;
drop table mo_user;
drop table mo_account;
drop table mo_role;
drop table mo_user_grant;
drop table mo_role_grant;
drop table mo_role_privs;
create table A(a int);
drop table mo_catalog.mo_database;
drop table mo_catalog.mo_tables;
drop table mo_catalog.mo_columns;
drop table mo_catalog.mo_user;
drop table mo_catalog.mo_account;
drop table mo_catalog.mo_role;
drop table mo_catalog.mo_user_grant;
drop table mo_catalog.mo_role_grant;
drop table mo_catalog.mo_role_privs;
create table mo_catalog.A(a int);
update mo_role_grant set granted_id = 0;
update mo_catalog.mo_role_grant set granted_id = 0;
insert into mo_role_grant values (100,101,0,0,"2022-10-09 00:00:00",true);
insert into mo_catalog.mo_role_grant values (100,101,0,0,"2022-10-09 00:00:00",true);
delete from mo_role_grant;
delete from mo_catalog.mo_role_grant;

use system;
drop table statement_info;
drop table span_info;
drop table log_info;
drop table error_info;
create table A(a int);
drop table system.statement_info;
drop table system.span_info;
drop table system.log_info;
drop table system.error_info;
create table system.A(a int);
update statement_info set statement_id = "1111";
update error_info set err_code = "1111";
update system.error_info set err_code = "1111";
insert into error_info values ("2022-10-09 00:00:00", "1", "1", "1", "1", "1");
insert into system.error_info values ("2022-10-09 00:00:00", "1", "1", "1", "1", "1");
delete from error_info;
delete from system.error_info;
delete from system.statement_info;

use system_metrics;
drop table sql_statement_total;
drop table sql_transaction_errors;
drop table sql_statement_errors;
drop table server_connections;
drop table process_cpu_percent;
drop table process_resident_memory_bytes;
drop table process_open_fds;
drop table process_max_fds;
drop table sys_cpu_seconds_total;
drop table sys_cpu_combined_percent;
drop table sys_memory_used;
drop table sys_memory_available;
drop table sys_disk_read_bytes;
drop table sys_disk_write_bytes;
drop table sys_net_recv_bytes;
drop table sys_net_sent_bytes;
create table A(a int);
drop table system_metrics.sql_statement_total;
drop table system_metrics.sql_transaction_errors;
drop table system_metrics.sql_statement_errors;
drop table system_metrics.server_connections;
drop table system_metrics.process_cpu_percent;
drop table system_metrics.process_resident_memory_bytes;
drop table system_metrics.process_open_fds;
drop table system_metrics.process_max_fds;
drop table system_metrics.sys_cpu_seconds_total;
drop table system_metrics.sys_cpu_combined_percent;
drop table system_metrics.sys_memory_used;
drop table system_metrics.sys_memory_available;
drop table system_metrics.sys_disk_read_bytes;
drop table system_metrics.sys_disk_write_bytes;
drop table system_metrics.sys_net_recv_bytes;
drop table system_metrics.sys_net_sent_bytes;
create table system_metrics.A(a int);
update sql_statement_total set type = "1";
update system_metrics.sql_statement_total set type = "1";
insert into sql_statement_total values ("2022-10-09 00:00:00",0,"1","1","1","1");
insert into system_metrics.sql_statement_total values ("2022-10-09 00:00:00",0,"1","1","1","1");

drop table if exists mysql.user;
drop table if exists mysql.db;
drop table if exists mysql.procs_priv;
drop table if exists mysql.columns_priv;
drop table if exists mysql.tables_priv;
-- add it when the mysql is ready
-- create table mysql.A(a int);
-- add update,insert,delete

use information_schema;
drop table if exists KEY_COLUMN_USAGE;
drop table if exists COLUMNS;
drop table if exists PROFILING;
drop table if exists `PROCESSLIST`;
drop table if exists USER_PRIVILEGES;
drop table if exists SCHEMATA;
drop table if exists CHARACTER_SETS;
drop table if exists TRIGGERS;
drop table if exists TABLES;
-- @bvt:issue#16438
drop table if exists PARTITIONS;
-- @bvt:issue
create table A(a int);
drop table if exists INFORMATION_SCHEMA.KEY_COLUMN_USAGE;
drop table if exists INFORMATION_SCHEMA.COLUMNS;
drop table if exists INFORMATION_SCHEMA.PROFILING;
drop table if exists INFORMATION_SCHEMA.`PROCESSLIST`;
drop table if exists INFORMATION_SCHEMA.USER_PRIVILEGES;
drop table if exists INFORMATION_SCHEMA.SCHEMATA;
drop table if exists INFORMATION_SCHEMA.CHARACTER_SETS;
drop table if exists INFORMATION_SCHEMA.TRIGGERS;
drop table if exists INFORMATION_SCHEMA.TABLES;
-- @bvt:issue#16438
drop table if exists INFORMATION_SCHEMA.PARTITIONS;
-- @bvt:issue
create table INFORMATION_SCHEMA.A(a int);
-- add update,insert,delete
set global enable_privilege_cache = on;