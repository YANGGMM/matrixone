drop pitr if exists pitr01;
create pitr pitr01 for account range 1 'h';
drop pitr if exists pitr02;
create pitr pitr02 for account range 1 'd';
drop pitr if exists pitr03;
create pitr pitr03 for account range 1 'mo';
drop pitr if exists pitr04;
create pitr pitr04 for account range 1 'y';
-- @ignore:1,2
show pitr;

-- cluster level success
drop pitr if exists pitr05;
create pitr pitr05 for cluster range 1 'h';
-- @ignore:1,2
show pitr;

-- failed
create pitr pitr01 for account range 1 'h';
create pitr if not exists pitr01 for account range 1 'h';
create pitr pitr07 for account acc01 database mo_catalog range 1 'h';
create pitr pitr08 for account acc01 table mo_catalog  mo_table range 1 'h';
create pitr pitr09 for account range 1 'yy';
create pitr pitr09 for account range -1 'h';
create pitr pitr09 for account range 2000 'h';
-- @ignore:1,2
show pitr;


--database level success
create database db01;
drop pitr if exists pitr10;
create pitr pitr10 for database db01 range 1 'h';
--database level failed
create pitr pitr11 for database db02 range 1 'h';
-- @ignore:1,2
show pitr;

--table level success
create table db01.table01 (col1 int);
drop pitr if exists pitr12;
create pitr pitr12 for table db01 table01 range 1 'h';
--table level failed
create pitr pitr13 for table db01 table02 range 1 'h';
-- @ignore:1,2
show pitr;

--sys to normal account level success
drop account if exists acc01;
create account acc01 admin_name = 'test_account' identified by '111';
drop pitr if exists pitr14;
create pitr pitr14 for account acc01 range 1 'h';
--sys to normal account level failed
create pitr pitr15 for account acc02 range 1 'h';
-- @ignore:1,2
show pitr;


-- normal account level success
-- @session:id=1&user=acc01:test_account&password=111
drop pitr if exists pitr16;
create pitr pitr16 for account range 1 'h';
-- @ignore:1,2
show pitr;

-- normal account level failed
create pitr pitr16 for account range 1 'h';
create pitr pitr16 if not exists range 1 'h';
create pitr pitr17 for cluster range 1 'h';
create pitr pitr18 for account acc01 range 1 'h';
-- @ignore:1,2
show pitr;

--database level success
create database db01;
drop pitr if exists pitr19;
create pitr pitr19 for database db01 range 1 'h';
--database level failed
create pitr pitr20 for database db02 range 1 'h';
-- @ignore:1,2
show pitr;

--table level success
create table db01.table01 (col1 int);
drop pitr if exists pitr21;
create pitr pitr21 for table db01 table01 range 1 'h';
--table level failed
create pitr pitr22 for table db01 table02 range 1 'h';
-- @ignore:1,2
show pitr;
-- @session

-- alter pitr success
alter pitr pitr01 range 1 'd';
-- alter pitr failed
alter pitr pitr100 range 1 'd';
alter pitr if exists pitr100 range 1 'd';
alter pitr pitr01 range 1 'yy';
alter pitr pitr01 range -1 'd';
alter pitr pitr01 range 2000 'd';
-- @ignore:1,2
show pitr;

-- drop pitr success
drop pitr pitr01;
-- drop pitr failed
drop pitr pitr100;
drop pitr if exists pitr100;
-- @ignore:1,2
show pitr;


-- @session:id=1&user=acc01:test_account&password=111
-- alter pitr success
alter pitr pitr16 range 1 'd';
-- alter pitr failed
alter pitr pitr100 range 1 'd';
alter pitr if exists pitr100 range 1 'd';
alter pitr pitr16 range 1 'yy';
alter pitr pitr16 range -1 'd';
alter pitr pitr16 range 2000 'd';
-- @ignore:1,2
show pitr;
-- drop pitr success
drop pitr pitr16;
-- drop pitr failed
drop pitr pitr100;
drop pitr if exists pitr100;
-- @ignore:1,2
show pitr;

drop pitr if exists pitr19;
drop pitr if exists pitr21;
-- @session


-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
drop account if exists acc01;
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
drop database if exists db01;
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
-- @ignore:1,2
drop pitr if exists pitr01;
drop pitr if exists pitr02;
drop pitr if exists pitr03;
drop pitr if exists pitr04;
drop pitr if exists pitr05;
drop pitr if exists pitr10;
drop pitr if exists pitr12;
drop pitr if exists pitr14;
-- @ignore:1,2
show pitr;
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';

drop account if exists acc02;
create account acc02 admin_name = 'test_account' identified by '111';
-- @session:id=2&user=acc02:test_account&password=111
create pitr pitr01 for account range 1 'h';
-- @ignore:1,2
show pitr;
select sleep(1);
alter pitr pitr01 range 1 'd';
-- @ignore:1,2
show pitr;
-- @session
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
drop account if exists acc02;
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';


drop pitr if exists pitr01;
create pitr pitr01 for account range 1 'h';
drop pitr if exists pitr02;
create pitr pitr02 for account range 1 'd';
drop pitr if exists pitr03;
create pitr pitr03 for account range 1 'mo';
drop pitr if exists pitr04;
create pitr pitr04 for account range 1 'y';
-- @ignore:1,2
show pitr;
-- @ignore:1,2
show pitr where ACCOUNT_NAME = 'sys';
-- @ignore:1,2
show pitr where ACCOUNT_NAME = 'sys' AND CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) > CAST_RANGE_VALUE_UNIT(1, 'h');
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) > 1;
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) > CAST_RANGE_VALUE_UNIT(1, 'h');
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) >= CAST_RANGE_VALUE_UNIT(29, 'd');
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) > CAST_RANGE_VALUE_UNIT(30, 'd');
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) >= CAST_RANGE_VALUE_UNIT(30, 'd');
-- @ignore:1,2
show pitr where CAST_RANGE_VALUE_UNIT(PITR_LENGTH, PITR_UNIT) > CAST_RANGE_VALUE_UNIT(11, 'mo');
drop pitr if exists pitr01;
drop pitr if exists pitr02;
drop pitr if exists pitr03;
drop pitr if exists pitr04;
-- @ignore:1,2
show pitr;

drop pitr if exists pitr05;
create pitr pitr05 for cluster range 1 'h';
drop pitr if exists pitr06;
create pitr pitr06 for cluster range 1 'd';


create database db01;
drop pitr if exists pitr10;
create pitr pitr10 for database db01 range 1 'h';
drop pitr if exists pitr11;
create pitr pitr11 for database db01 range 1 'd';

create table db01.table01 (col1 int);
drop pitr if exists pitr12;
create pitr pitr12 for table db01 table01 range 1 'h';
drop pitr if exists pitr13;
create pitr pitr13 for table db01 table01 range 1 'd';

drop account if exists acc01;
create account acc01 admin_name = 'test_account' identified by '111';
drop pitr if exists pitr14;
create pitr pitr14 for account acc01 range 1 'h';
drop pitr if exists pitr15;
create pitr pitr15 for account acc01 range 1 'd';

drop database if exists db01;
drop account if exists acc01;
drop pitr if exists pitr05;
drop pitr if exists pitr06;
drop pitr if exists pitr10;
drop pitr if exists pitr11;
drop pitr if exists pitr12;
drop pitr if exists pitr13;
drop pitr if exists pitr14;
drop pitr if exists pitr15;
-- @ignore:1,2
show pitr;

drop account if exists acc01;
create account acc01 admin_name = 'test_account' identified by '111';
-- @session:id=3&user=acc01:test_account&password=111
drop pitr if exists pitr01;
create pitr pitr01 for account range 1 'h';
drop pitr if exists pitr02;
create pitr pitr02 for account range 1 'd';

create database db01;
drop pitr if exists pitr10;
create pitr pitr10 for database db01 range 1 'h';
drop pitr if exists pitr11;
create pitr pitr11 for database db01 range 1 'd';

create table db01.table01 (col1 int);
drop pitr if exists pitr12;
create pitr pitr12 for table db01 table01 range 1 'h';
drop pitr if exists pitr13;
create pitr pitr13 for table db01 table01 range 1 'd';

drop pitr if exists pitr01;
drop pitr if exists pitr02;
drop pitr if exists pitr10;
drop pitr if exists pitr11;
drop pitr if exists pitr12;
drop pitr if exists pitr13;

drop database if exists db01;
-- @session
drop account if exists acc01;

drop pitr if exists pitr111;
create pitr pitr111 for account range 1 'h';
drop pitr if exists pitr122;
create pitr pitr122 for account range 1 'd';
drop pitr if exists pitr111;
drop pitr if exists pitr122;

create database db01;
drop pitr if exists pitr10;
create pitr pitr10 for database db01 range 1 'h';
drop pitr if exists pitr11;
create pitr pitr11 for database db01 range 1 'd';
drop database if exists db01;
create database db01;
drop pitr if exists pitr12;
create pitr pitr12 for database db01 range 1 'd';
drop pitr if exists pitr10;
drop pitr if exists pitr11;
drop pitr if exists pitr12;
drop database if exists db01;

-- @ignore:1,2
show pitr;
create pitr sys_mo_catalog_pitr for account range 1 'h';
drop pitr if exists sys_mo_catalog_pitr;
drop pitr if exists pitr01;
create pitr pitr01 for account range 1 'h';
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
-- @ignore:1,2
show pitr;
drop pitr if exists pitr01;
drop pitr if exists pitr02;
create pitr pitr02 for account range 1 'd';
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
-- @ignore:1,2
show pitr;
drop pitr if exists pitr02;
drop account if exists acc01;
create account acc01 admin_name = 'test_account' identified by '111';
-- @session:id=4&user=acc01:test_account&password=111
create pitr pitr01 for account range 1 'mo';
-- @ignore:1,2
show pitr;
-- @session
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
drop account if exists acc01;
drop database if exists db01;
create database db01;
drop pitr if exists pitr10;
create pitr pitr10 for database db01 range 1 'y';
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
-- @ignore:1,2
show pitr;
drop pitr if exists pitr10;
-- @ignore:0,2,3,4,6,7,10
select `pitr_id`, `pitr_name`, `create_account`, `create_time`, `modified_time`, `level`, `account_id`, `account_name`, `database_name`, `table_name`, `obj_id`, `pitr_length`, `pitr_unit` from mo_catalog.mo_pitr Where pitr_name != 'sys_mo_catalog_pitr';
drop database if exists db01;
