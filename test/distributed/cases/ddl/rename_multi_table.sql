drop database if exists abc;
create database abc;
use abc;
create table rename_table_01(a int primary key auto_increment,b varchar(10));
create table rename_table_02(a int primary key auto_increment,b varchar(10));
create table rename_table_03(a int primary key auto_increment,b varchar(10));
create table rename_table_04(a int primary key auto_increment,b varchar(10));
create table rename_table_05(a int primary key auto_increment,b varchar(10));

insert into rename_table_01(b) values ('key');
insert into rename_table_02(b) values ('key');
insert into rename_table_03(b) values ('key');
insert into rename_table_04(b) values ('key');
insert into rename_table_05(b) values ('key');
show tables;

rename table rename_table_01 to rename01,rename_table_02 to rename02,rename_table_03 to rename03,rename_table_04 to rename04,rename_table_05 to rename05;
show tables;

drop database if exists abc;
