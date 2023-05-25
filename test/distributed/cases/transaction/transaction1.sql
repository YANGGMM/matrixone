create database transaction_enhance;
use transaction_enhance;
-- truncate table
drop table if exists atomic_table_10;
create table atomic_table_10(c1 int,c2 varchar(25));
insert into atomic_table_10 values (3,"a"),(4,"b"),(5,"c");
start transaction ;
truncate table atomic_table_10;
-- @session:id=2&user=sys:dump&password=111
use transaction_enhance;
select * from atomic_table_10;
-- @session
select * from atomic_table_10;
commit;
select * from atomic_table_10;

drop table if exists atomic_table_10;
create table atomic_table_10(c1 int,c2 varchar(25));
insert into atomic_table_10 values (3,"a"),(4,"b"),(5,"c");
start transaction ;
truncate table atomic_table_10;
-- @session:id=2&user=sys:dump&password=111
select * from atomic_table_10;
-- @session
select * from atomic_table_10;
rollback;
select * from atomic_table_10;

drop table if exists atomic_table_10;
create table atomic_table_10(c1 int,c2 varchar(25));
insert into atomic_table_10 values (3,"a"),(4,"b"),(5,"c");
begin ;
truncate table atomic_table_10;
-- @bvt:issue#8848
-- @session:id=2&user=sys:dump&password=111
insert into atomic_table_10 values (6,"a"),(7,"b"),(8,"c");
select * from atomic_table_10;
-- @session
-- @bvt:issue
select * from atomic_table_10;
commit;
select * from atomic_table_10;

drop table if exists atomic_table_11;
create table atomic_table_11(c1 int,c2 varchar(25));
insert into atomic_table_11 values (3,"a"),(4,"b"),(5,"c");
begin;
drop table atomic_table_11;
-- @bvt:issue#9429
-- @session:id=2&user=sys:dump&password=111
insert into atomic_table_11 values (6,"a");
select * from atomic_table_11;
-- @session
-- @bvt:issue
commit;
select * from atomic_table_11;

drop table if exists atomic_table_11;
create table atomic_table_11(c1 int,c2 varchar(25));
insert into atomic_table_11 values (3,"a"),(4,"b"),(5,"c");
begin;
drop table atomic_table_11;
-- @bvt:issue#9429
-- @session:id=2&user=sys:dump&password=111
insert into atomic_table_11 values (6,"a");
select * from atomic_table_11;
-- @session
rollback ;
select * from atomic_table_11;
-- @bvt:issue
drop table if exists atomic_table_11;
create table atomic_table_11(c1 int,c2 varchar(25));
insert into atomic_table_11 values (3,"a"),(4,"b"),(5,"c");
begin;
-- @session:id=2&user=sys:dump&password=111
drop table atomic_table_11;
-- @session
drop table atomic_table_11;
commit;

--alter table
drop table if exists atomic_table_12;
create table atomic_table_12(c1 int,c2 varchar(25));
insert into atomic_table_12 values (3,"a"),(4,"b"),(5,"c");
begin;
alter table atomic_table_12 add index key1(c1);
alter table atomic_table_12 alter index key1 visible;
-- @session:id=2&user=sys:dump&password=111
show create table atomic_table_12;
-- @session
commit;
show create table atomic_table_12;
show index from atomic_table_12;

use transaction_enhance;
drop table if exists atomic_table_12_1;
create table atomic_table_12_1(c1 int,c2 varchar(25));
insert into atomic_table_12_1 values (3,"a"),(4,"b"),(5,"c");
begin;
alter table atomic_table_12_1 add index key1(c1);
alter table atomic_table_12_1 alter index key1 visible;
-- @session:id=2&user=sys:dump&password=111
show create table atomic_table_12_1;
-- @session
rollback;
show create table atomic_table_12_1;
show index from atomic_table_12_1;