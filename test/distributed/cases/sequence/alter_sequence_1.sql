create sequence alter_seq_01  as smallint;
show sequences;
alter sequence alter_seq_01 as bigint;
show sequences;
select nextval('alter_seq_01'),currval('alter_seq_01');
alter sequence alter_seq_01 minvalue 1 maxvalue 2;
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
alter sequence alter_seq_01 cycle;
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
alter sequence alter_seq_01 maxvalue 1000;
alter sequence alter_seq_01 increment by 10;
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
alter sequence alter_seq_01 start with 900;
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
select nextval('alter_seq_01'),currval('alter_seq_01');
drop sequence alter_seq_01;

create sequence alter_seq_02 increment 3;
select nextval('alter_seq_02');
select nextval('alter_seq_02'),currval('alter_seq_02');
alter sequence alter_seq_02 increment 10;
select nextval('alter_seq_02'),currval('alter_seq_02');
drop sequence alter_seq_02;
create sequence alter_seq_03 start 1000;
select nextval('alter_seq_03'),currval('alter_seq_03');
select nextval('alter_seq_03'),currval('alter_seq_03');
select nextval('alter_seq_03'),currval('alter_seq_03');
alter sequence alter_seq_03 start 1001;
select nextval('alter_seq_03'),currval('alter_seq_03');
drop sequence alter_seq_03;
create sequence alter_seq_03 increment by 10;
alter sequence alter_seq_03;
drop sequence alter_seq_03;