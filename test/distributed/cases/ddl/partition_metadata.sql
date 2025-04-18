-- @skip:issue#16438
drop database if exists db1;
create database db1;
use db1;

drop table if exists lc;
CREATE TABLE lc (
                    a INT NULL,
                    b INT NULL
)
    PARTITION BY LIST COLUMNS(a,b) (
	PARTITION p0 VALUES IN( (0,0), (NULL,NULL) ),
	PARTITION p1 VALUES IN( (0,1), (0,2), (0,3), (1,1), (1,2) ),
	PARTITION p2 VALUES IN( (1,0), (2,0), (2,1), (3,0), (3,1) ),
	PARTITION p3 VALUES IN( (1,3), (2,2), (2,3), (3,2), (3,3) )
);

select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 'lc' and table_schema = 'db1';
drop table lc;

drop table if exists client_firms;
CREATE TABLE client_firms (
                              id   INT,
                              name VARCHAR(35)
)
    PARTITION BY LIST (id) (
	PARTITION r0 VALUES IN (1, 5, 9, 13, 17, 21),
	PARTITION r1 VALUES IN (2, 6, 10, 14, 18, 22),
	PARTITION r2 VALUES IN (3, 7, 11, 15, 19, 23),
	PARTITION r3 VALUES IN (4, 8, 12, 16, 20, 24)
);

select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 'client_firms' and table_schema = 'db1';
drop table client_firms;

drop table if exists tk;
CREATE TABLE tk (col1 INT, col2 CHAR(5), col3 DATE) PARTITION BY KEY(col1, col2) PARTITIONS 4;
select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 'tk' and table_schema = 'db1';
drop table tk;

drop table if exists t1;
CREATE TABLE t1 (col1 INT, col2 CHAR(5), col3 DATE) PARTITION BY LINEAR HASH( YEAR(col3)) PARTITIONS 6;
select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 't1' and table_schema = 'db1';
drop table t1;

drop table if exists employees;
CREATE TABLE employees (
                           emp_no      INT             NOT NULL,
                           birth_date  DATE            NOT NULL,
                           first_name  VARCHAR(14)     NOT NULL,
                           last_name   VARCHAR(16)     NOT NULL,
                           gender      varchar(5)      NOT NULL,
                           hire_date   DATE            NOT NULL,
                           PRIMARY KEY (emp_no)
)
    partition by range columns (emp_no)
(
    partition p01 values less than (100001),
    partition p02 values less than (270001),
    partition p03 values less than (450001),
    partition p04 values less than (530001),
    partition p05 values less than (610001),
    partition p06 values less than (MAXVALUE)
);

select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 'employees' and table_schema = 'db1';
drop table employees;

drop table if exists trp;
CREATE TABLE trp (
                     id INT NOT NULL,
                     fname VARCHAR(30),
                     lname VARCHAR(30),
                     hired DATE NOT NULL DEFAULT '1970-01-01',
                     separated DATE NOT NULL DEFAULT '9999-12-31',
                     job_code INT,
                     store_id INT
)
    PARTITION BY RANGE ( YEAR(separated) ) (
	PARTITION p0 VALUES LESS THAN (1991),
	PARTITION p1 VALUES LESS THAN (1996),
	PARTITION p2 VALUES LESS THAN (2001),
	PARTITION p3 VALUES LESS THAN MAXVALUE
);

select
    table_catalog,
    table_schema,
    table_name,
    partition_name,
    partition_ordinal_position,
    partition_method,
    partition_expression,
    partition_description,
    table_rows,
    avg_row_length,
    data_length,
    max_data_length,
    partition_comment
from information_schema.partitions
where table_name = 'trp' and table_schema = 'db1';
drop table trp;