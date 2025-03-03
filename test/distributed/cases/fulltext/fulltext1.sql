set experimental_fulltext_index=1;
set ft_relevancy_algorithm="TF-IDF";
drop database if exists test;
create database test;
use test;
create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title VARCHAR(200), body TEXT, FULLTEXT (title,body));
show create table articles;
drop table articles;

create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title VARCHAR(200), body TEXT);
create fulltext index fdx_01 on articles(title, body);
drop index fdx_01 on articles;
create fulltext index fdx_02 on articles(title);
drop  index fdx_02 on articles;
create fulltext index fdx_03 on articles(id);
drop  index fdx_04 on articles(title, body) with PARSER ngram;
drop  index fdx_04 on articles;
drop table articles;

create table src (id bigint primary key, json1 json, json2 json);
create fulltext index ftidx1 on src(json1) with parser json;
show create table src;
alter table src drop column json1;
drop table src;

create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title VARCHAR(200), body TEXT);
insert into articles (title,body) VALUES ('MySQL Tutorial','DBMS stands for DataBase ...'),
                                         ('How To Use MySQL Well','After you went through a ...'),
                                         ('Optimizing MySQL','In this tutorial, we show ...'),
                                         ('1001 MySQL Tricks','1. Never run mysqld as root. 2. ...'),
                                         ('MySQL vs. YourSQL','In the following database comparison ...'),
                                         ('MySQL Security','When configured properly, MySQL ...');
create fulltext index fdx_01 on articles(title, body) with parser ngram;
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
drop index fdx_01 on articles;
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
create fulltext index fdx_01 on articles(title, body);
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
select count(*) from (select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE));
drop table articles;

create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title VARCHAR(200), body TEXT);
insert into articles (title,body) VALUES ('神雕侠侣 第一回 风月无情','越女采莲秋水畔，窄袖轻罗，暗露双金钏 ...'),
                                         ('神雕侠侣 第二回 故人之子','正自发痴，忽听左首屋中传出一人喝道：“这是在人家府上，你又提小龙女干什么？” ...'),
                                         ('神雕侠侣 第三回 投师终南','郭靖在舟中潜运神功，数日间伤势便已痊愈了大半。 ...'),
                                         ('神雕侠侣 第四回 全真门下','郭靖摆脱众道纠缠，提气向重阳宫奔去，忽听得钟声镗镗响起 ...'),
                                         ('神雕侠侣 第五回 活死人墓','杨过摔下山坡，滚入树林长草丛中，便即昏晕 ...'),
                                         ('神雕侠侣 第六回 玉女心经','小龙女从怀里取出一个瓷瓶，交在杨过手里 ...');
create fulltext index fdx_01 on articles(title, body) with parser ngram;
select * from articles where match(title,body)  AGAINST ('风月无情' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('杨过' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('小龙女' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('神雕侠侣' IN NATURAL LANGUAGE MODE);
drop table articles;

create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title json, body json);
insert into articles (title,body) VALUES ('{"title": "MySQL Tutorial"}','{"body":"DBMS stands for DataBase ..."}'),
                                         ('{"title":"How To Use MySQL Well"}','{"body":"After you went through a ..."}'),
                                         ('{"title":"Optimizing MySQL"}','{"body":"In this tutorial, we show ..."}'),
                                         ('{"title":"1001 MySQL Tricks"}','{"body":"1. Never run mysqld as root. 2. ..."}'),
                                         ('{"title":"MySQL vs. YourSQL"}','{"body":"In the following database comparison ..."}'),
                                         ('{"title":"MySQL Security"}','{"body":"When configured properly, MySQL ..."}');
create fulltext index fdx_01 on articles(title, body) with parser json;
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
drop index fdx_01 on articles;
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
create fulltext index fdx_01 on articles(title, body);
select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE) union select * from articles where match(title,body)  AGAINST ('YourSQL' IN NATURAL LANGUAGE MODE) order by id;
select count(*) from (select * from articles where match(title,body)  AGAINST ('database' IN NATURAL LANGUAGE MODE));
drop table articles;

create table articles (id INT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY, title json, body json);
insert into articles (title,body) VALUES ('{"title": "神雕侠侣 第一回 风月无情"}','{"body":"越女采莲秋水畔，窄袖轻罗，暗露双金钏 ..."}'),
                                         ('{"title":"神雕侠侣 第二回 故人之子"}','{"body":"正自发痴，忽听左首屋中传出一人喝道：“这是在人家府上，你又提小龙女干什么？” ..."}'),
                                         ('{"title":"神雕侠侣 第三回 投师终南"}','{"body":"郭靖在舟中潜运神功，数日间伤势便已痊愈了大半。 ..."}'),
                                         ('{"title":"神雕侠侣 第四回 全真门下"}','{"body":"郭靖摆脱众道纠缠，提气向重阳宫奔去，忽听得钟声镗镗响起 ..."}'),
                                         ('{"title":"神雕侠侣 第五回 活死人墓"}','{"body":"杨过摔下山坡，滚入树林长草丛中，便即昏晕 ..."}'),
                                         ('{"title":"神雕侠侣 第六回 玉女心经"}','{"body":"小龙女从怀里取出一个瓷瓶，交在杨过手里 ..."}');
create fulltext index fdx_01 on articles(title, body) with parser json;
select * from articles where match(title,body)  AGAINST ('风月无情' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('杨过' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('小龙女' IN NATURAL LANGUAGE MODE);
select * from articles where match(title,body)  AGAINST ('神雕侠侣' IN NATURAL LANGUAGE MODE);
drop table articles;

drop table if exists t1;
create table t1(a int primary key, b varchar(200), c int);
insert into t1(a,b,c) select result, "test create big fulltext index" ,result from generate_series(10000) g;
create fulltext index ftidx on t1 (b);
create index index2 on t1 (c);
-- @separator:table
explain select * from t1 where c = 100;
drop table t1;

drop database test;