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