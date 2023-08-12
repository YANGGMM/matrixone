create account acc_idx ADMIN_NAME 'root' IDENTIFIED BY '123456';
-- @session:id=2&user=acc_idx:root&password=123456
drop role if exists role_r1;
drop user if exists role_u1;
create role role_r1;
create user role_u1 identified by '111' default role role_r1;
create database test;
use test;
drop table if exists test01;
create table test01(a int);
insert into test01 values(1);
insert into test01 values(2);
-- @session

-- @session:id=3&user=acc_idx:role_u1:role_r1&password=111
create database abc;
show databases like 'test';
select * from test.test01;
-- @session

-- @session:id=4&user=acc_idx:root&password=123456
grant create database on account * to role_r1;
grant show databases on account * to role_r1;
grant connect on account * to role_r1;
grant select on table * to role_r1;
-- @session

-- @session:id=5&user=acc_idx:role_u1:role_r1&password=111
create database abc;
show databases like 'test';
select * from test.test01;
-- @session

-- @session:id=6&user=acc_idx:root&password=123456
revoke if exists show databases on account * from role_r1;
revoke if exists select on table * from role_r1;
revoke if exists create database  on account * from role_r1;
-- @session

-- @session:id=7&user=acc_idx:role_u1:role_r1&password=111
show databases like 'test';
select * from test.test01;
create database abc1;
show grants for 'role_u1'@"localhost";
-- @session

-- @session:id=8&user=acc_idx:root&password=123456
select role_name, privilege_name from mo_catalog.mo_role_privs where role_name = 'role_r1';
drop database test;
drop user role_u1;
drop role role_r1;
create database db1;
create role r1;
grant create database on account * to r1;
grant create table on database db1 to r1;
grant insert on table db1.* to r1;
grant select on table db1.* to r1;
create user u1 identified by '111' default role r1;
-- @session

-- @session:id=9&user=acc_idx:u1:r1&password=111
create database test;
create table test.test01(a int);
insert into test.test01 values(1);
insert into test.test01 values(2);
insert into test.test01 values(3);
select * from test.test01;
-- @session

-- @session:id=10&user=acc_idx:root&password=123456
revoke if exists create database on account * from r1;
revoke if exists create table on database db1 from r1;
revoke if exists insert on table db1.* from r1;
revoke if exists select on table db1.* from r1;
-- @session

-- @session:id=11&user=acc_idx:u1:r1&password=111
create database revokeTest;
create table test.test02(a int);
insert into test.test01 values(4);
select * from test.test01;
show grants for 'u1'@"localhost";
-- @session

-- @session:id=12&user=acc_idx:root&password=123456
select role_name, privilege_name from mo_catalog.mo_role_privs where role_name = 'r1';
drop role r1;
drop user u1;
drop database test;
-- @session

drop account acc_idx;