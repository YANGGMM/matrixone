create database if not exists mysql_ddl_test_db;
use mysql_ddl_test_db;
DROP TABLE IF EXISTS `mysql_ddl_test_t32`;
CREATE TABLE `mysql_ddl_test_t32` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键id',
  `plt` int(11) DEFAULT '0' COMMENT '平台id',
  `device_id` varchar(32) DEFAULT '' COMMENT '设备id',
  `user_id` int(11) DEFAULT '0' COMMENT '用户id',
  `home_id` int(11) DEFAULT '0' COMMENT '家庭id',
  `type` varchar(32) DEFAULT '' COMMENT '视频类型 CRY, MOVE, VOICE, STATE, LOW_BATTERY',
  `video_image` varchar(500) DEFAULT '' COMMENT '封面链接',
  `video_url` varchar(1024) DEFAULT '' COMMENT '视频链接',
  `video_start` char(12) NOT NULL DEFAULT '' COMMENT '视频开始时间戳',
  `video_end` char(12) NOT NULL DEFAULT '' COMMENT '视频结束时间戳',
  `status` tinyint(4) DEFAULT '0' COMMENT '0未读1已读',
  `visible` tinyint(4) NOT NULL DEFAULT '0' COMMENT '未购买套餐不可见 0不可见1可见',
  `create_time` char(12) NOT NULL DEFAULT '' COMMENT '创建时间戳',
  `created_at` datetime NOT NULL COMMENT '创建时间',
  `updated_at` datetime DEFAULT NULL COMMENT '更新时间',
  `content` text,
  `event_start` char(12) NOT NULL DEFAULT '' COMMENT '事件开始时间戳',
  `event_end` char(12) NOT NULL DEFAULT '' COMMENT '事件结束时间戳',
  `msg_id` varchar(32) NOT NULL DEFAULT '',
  `event_id` varchar(32) NOT NULL DEFAULT '',
  `accept` tinyint(4) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`,`created_at`) USING BTREE,
  KEY `device_id` (`device_id`) USING BTREE,
  KEY `userId` (`user_id`) USING BTREE,
  KEY `createTime` (`create_time`) USING BTREE,
  KEY `plt_dev_user_sv_ctime` (`plt`,`status`,`visible`) USING BTREE,
  KEY `created` (`created_at`) USING BTREE,
  KEY `index_0` (`user_id`,`status`,`create_time`) USING BTREE
) ENGINE=InnoDB AUTO_INCREMENT=2122298806 DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC COMMENT='视频'
/*!50100 PARTITION BY LIST ((TO_DAYS(created_at)*24 + HOUR(created_at)) % (7*24))
(PARTITION hour0 VALUES IN (0) ENGINE = InnoDB,
 PARTITION hour1 VALUES IN (1) ENGINE = InnoDB,
 PARTITION hour2 VALUES IN (2) ENGINE = InnoDB,
 PARTITION hour3 VALUES IN (3) ENGINE = InnoDB,
 PARTITION hour4 VALUES IN (4) ENGINE = InnoDB,
 PARTITION hour5 VALUES IN (5) ENGINE = InnoDB,
 PARTITION hour6 VALUES IN (6) ENGINE = InnoDB,
 PARTITION hour7 VALUES IN (7) ENGINE = InnoDB,
 PARTITION hour8 VALUES IN (8) ENGINE = InnoDB,
 PARTITION hour9 VALUES IN (9) ENGINE = InnoDB,
 PARTITION hour10 VALUES IN (10) ENGINE = InnoDB,
 PARTITION hour11 VALUES IN (11) ENGINE = InnoDB,
 PARTITION hour12 VALUES IN (12) ENGINE = InnoDB,
 PARTITION hour13 VALUES IN (13) ENGINE = InnoDB,
 PARTITION hour14 VALUES IN (14) ENGINE = InnoDB,
 PARTITION hour15 VALUES IN (15) ENGINE = InnoDB,
 PARTITION hour16 VALUES IN (16) ENGINE = InnoDB,
 PARTITION hour17 VALUES IN (17) ENGINE = InnoDB,
 PARTITION hour18 VALUES IN (18) ENGINE = InnoDB,
 PARTITION hour19 VALUES IN (19) ENGINE = InnoDB,
 PARTITION hour20 VALUES IN (20) ENGINE = InnoDB,
 PARTITION hour21 VALUES IN (21) ENGINE = InnoDB,
 PARTITION hour22 VALUES IN (22) ENGINE = InnoDB,
 PARTITION hour23 VALUES IN (23) ENGINE = InnoDB,
 PARTITION hour24 VALUES IN (24) ENGINE = InnoDB,
 PARTITION hour25 VALUES IN (25) ENGINE = InnoDB,
 PARTITION hour26 VALUES IN (26) ENGINE = InnoDB,
 PARTITION hour27 VALUES IN (27) ENGINE = InnoDB,
 PARTITION hour28 VALUES IN (28) ENGINE = InnoDB,
 PARTITION hour29 VALUES IN (29) ENGINE = InnoDB,
 PARTITION hour30 VALUES IN (30) ENGINE = InnoDB,
 PARTITION hour31 VALUES IN (31) ENGINE = InnoDB,
 PARTITION hour32 VALUES IN (32) ENGINE = InnoDB,
 PARTITION hour33 VALUES IN (33) ENGINE = InnoDB,
 PARTITION hour34 VALUES IN (34) ENGINE = InnoDB,
 PARTITION hour35 VALUES IN (35) ENGINE = InnoDB,
 PARTITION hour36 VALUES IN (36) ENGINE = InnoDB,
 PARTITION hour37 VALUES IN (37) ENGINE = InnoDB,
 PARTITION hour38 VALUES IN (38) ENGINE = InnoDB,
 PARTITION hour39 VALUES IN (39) ENGINE = InnoDB,
 PARTITION hour40 VALUES IN (40) ENGINE = InnoDB,
 PARTITION hour41 VALUES IN (41) ENGINE = InnoDB,
 PARTITION hour42 VALUES IN (42) ENGINE = InnoDB,
 PARTITION hour43 VALUES IN (43) ENGINE = InnoDB,
 PARTITION hour44 VALUES IN (44) ENGINE = InnoDB,
 PARTITION hour45 VALUES IN (45) ENGINE = InnoDB,
 PARTITION hour46 VALUES IN (46) ENGINE = InnoDB,
 PARTITION hour47 VALUES IN (47) ENGINE = InnoDB,
 PARTITION hour48 VALUES IN (48) ENGINE = InnoDB,
 PARTITION hour49 VALUES IN (49) ENGINE = InnoDB,
 PARTITION hour50 VALUES IN (50) ENGINE = InnoDB,
 PARTITION hour51 VALUES IN (51) ENGINE = InnoDB,
 PARTITION hour52 VALUES IN (52) ENGINE = InnoDB,
 PARTITION hour53 VALUES IN (53) ENGINE = InnoDB,
 PARTITION hour54 VALUES IN (54) ENGINE = InnoDB,
 PARTITION hour55 VALUES IN (55) ENGINE = InnoDB,
 PARTITION hour56 VALUES IN (56) ENGINE = InnoDB,
 PARTITION hour57 VALUES IN (57) ENGINE = InnoDB,
 PARTITION hour58 VALUES IN (58) ENGINE = InnoDB,
 PARTITION hour59 VALUES IN (59) ENGINE = InnoDB,
 PARTITION hour60 VALUES IN (60) ENGINE = InnoDB,
 PARTITION hour61 VALUES IN (61) ENGINE = InnoDB,
 PARTITION hour62 VALUES IN (62) ENGINE = InnoDB,
 PARTITION hour63 VALUES IN (63) ENGINE = InnoDB,
 PARTITION hour64 VALUES IN (64) ENGINE = InnoDB,
 PARTITION hour65 VALUES IN (65) ENGINE = InnoDB,
 PARTITION hour66 VALUES IN (66) ENGINE = InnoDB,
 PARTITION hour67 VALUES IN (67) ENGINE = InnoDB,
 PARTITION hour68 VALUES IN (68) ENGINE = InnoDB,
 PARTITION hour69 VALUES IN (69) ENGINE = InnoDB,
 PARTITION hour70 VALUES IN (70) ENGINE = InnoDB,
 PARTITION hour71 VALUES IN (71) ENGINE = InnoDB,
 PARTITION hour72 VALUES IN (72) ENGINE = InnoDB,
 PARTITION hour73 VALUES IN (73) ENGINE = InnoDB,
 PARTITION hour74 VALUES IN (74) ENGINE = InnoDB,
 PARTITION hour75 VALUES IN (75) ENGINE = InnoDB,
 PARTITION hour76 VALUES IN (76) ENGINE = InnoDB,
 PARTITION hour77 VALUES IN (77) ENGINE = InnoDB,
 PARTITION hour78 VALUES IN (78) ENGINE = InnoDB,
 PARTITION hour79 VALUES IN (79) ENGINE = InnoDB,
 PARTITION hour80 VALUES IN (80) ENGINE = InnoDB,
 PARTITION hour81 VALUES IN (81) ENGINE = InnoDB,
 PARTITION hour82 VALUES IN (82) ENGINE = InnoDB,
 PARTITION hour83 VALUES IN (83) ENGINE = InnoDB,
 PARTITION hour84 VALUES IN (84) ENGINE = InnoDB,
 PARTITION hour85 VALUES IN (85) ENGINE = InnoDB,
 PARTITION hour86 VALUES IN (86) ENGINE = InnoDB,
 PARTITION hour87 VALUES IN (87) ENGINE = InnoDB,
 PARTITION hour88 VALUES IN (88) ENGINE = InnoDB,
 PARTITION hour89 VALUES IN (89) ENGINE = InnoDB,
 PARTITION hour90 VALUES IN (90) ENGINE = InnoDB,
 PARTITION hour91 VALUES IN (91) ENGINE = InnoDB,
 PARTITION hour92 VALUES IN (92) ENGINE = InnoDB,
 PARTITION hour93 VALUES IN (93) ENGINE = InnoDB,
 PARTITION hour94 VALUES IN (94) ENGINE = InnoDB,
 PARTITION hour95 VALUES IN (95) ENGINE = InnoDB,
 PARTITION hour96 VALUES IN (96) ENGINE = InnoDB,
 PARTITION hour97 VALUES IN (97) ENGINE = InnoDB,
 PARTITION hour98 VALUES IN (98) ENGINE = InnoDB,
 PARTITION hour99 VALUES IN (99) ENGINE = InnoDB,
 PARTITION hour100 VALUES IN (100) ENGINE = InnoDB,
 PARTITION hour101 VALUES IN (101) ENGINE = InnoDB,
 PARTITION hour102 VALUES IN (102) ENGINE = InnoDB,
 PARTITION hour103 VALUES IN (103) ENGINE = InnoDB,
 PARTITION hour104 VALUES IN (104) ENGINE = InnoDB,
 PARTITION hour105 VALUES IN (105) ENGINE = InnoDB,
 PARTITION hour106 VALUES IN (106) ENGINE = InnoDB,
 PARTITION hour107 VALUES IN (107) ENGINE = InnoDB,
 PARTITION hour108 VALUES IN (108) ENGINE = InnoDB,
 PARTITION hour109 VALUES IN (109) ENGINE = InnoDB,
 PARTITION hour110 VALUES IN (110) ENGINE = InnoDB,
 PARTITION hour111 VALUES IN (111) ENGINE = InnoDB,
 PARTITION hour112 VALUES IN (112) ENGINE = InnoDB,
 PARTITION hour113 VALUES IN (113) ENGINE = InnoDB,
 PARTITION hour114 VALUES IN (114) ENGINE = InnoDB,
 PARTITION hour115 VALUES IN (115) ENGINE = InnoDB,
 PARTITION hour116 VALUES IN (116) ENGINE = InnoDB,
 PARTITION hour117 VALUES IN (117) ENGINE = InnoDB,
 PARTITION hour118 VALUES IN (118) ENGINE = InnoDB,
 PARTITION hour119 VALUES IN (119) ENGINE = InnoDB,
 PARTITION hour120 VALUES IN (120) ENGINE = InnoDB,
 PARTITION hour121 VALUES IN (121) ENGINE = InnoDB,
 PARTITION hour122 VALUES IN (122) ENGINE = InnoDB,
 PARTITION hour123 VALUES IN (123) ENGINE = InnoDB,
 PARTITION hour124 VALUES IN (124) ENGINE = InnoDB,
 PARTITION hour125 VALUES IN (125) ENGINE = InnoDB,
 PARTITION hour126 VALUES IN (126) ENGINE = InnoDB,
 PARTITION hour127 VALUES IN (127) ENGINE = InnoDB,
 PARTITION hour128 VALUES IN (128) ENGINE = InnoDB,
 PARTITION hour129 VALUES IN (129) ENGINE = InnoDB,
 PARTITION hour130 VALUES IN (130) ENGINE = InnoDB,
 PARTITION hour131 VALUES IN (131) ENGINE = InnoDB,
 PARTITION hour132 VALUES IN (132) ENGINE = InnoDB,
 PARTITION hour133 VALUES IN (133) ENGINE = InnoDB,
 PARTITION hour134 VALUES IN (134) ENGINE = InnoDB,
 PARTITION hour135 VALUES IN (135) ENGINE = InnoDB,
 PARTITION hour136 VALUES IN (136) ENGINE = InnoDB,
 PARTITION hour137 VALUES IN (137) ENGINE = InnoDB,
 PARTITION hour138 VALUES IN (138) ENGINE = InnoDB,
 PARTITION hour139 VALUES IN (139) ENGINE = InnoDB,
 PARTITION hour140 VALUES IN (140) ENGINE = InnoDB,
 PARTITION hour141 VALUES IN (141) ENGINE = InnoDB,
 PARTITION hour142 VALUES IN (142) ENGINE = InnoDB,
 PARTITION hour143 VALUES IN (143) ENGINE = InnoDB,
 PARTITION hour144 VALUES IN (144) ENGINE = InnoDB,
 PARTITION hour145 VALUES IN (145) ENGINE = InnoDB,
 PARTITION hour146 VALUES IN (146) ENGINE = InnoDB,
 PARTITION hour147 VALUES IN (147) ENGINE = InnoDB,
 PARTITION hour148 VALUES IN (148) ENGINE = InnoDB,
 PARTITION hour149 VALUES IN (149) ENGINE = InnoDB,
 PARTITION hour150 VALUES IN (150) ENGINE = InnoDB,
 PARTITION hour151 VALUES IN (151) ENGINE = InnoDB,
 PARTITION hour152 VALUES IN (152) ENGINE = InnoDB,
 PARTITION hour153 VALUES IN (153) ENGINE = InnoDB,
 PARTITION hour154 VALUES IN (154) ENGINE = InnoDB,
 PARTITION hour155 VALUES IN (155) ENGINE = InnoDB,
 PARTITION hour156 VALUES IN (156) ENGINE = InnoDB,
 PARTITION hour157 VALUES IN (157) ENGINE = InnoDB,
 PARTITION hour158 VALUES IN (158) ENGINE = InnoDB,
 PARTITION hour159 VALUES IN (159) ENGINE = InnoDB,
 PARTITION hour160 VALUES IN (160) ENGINE = InnoDB,
 PARTITION hour161 VALUES IN (161) ENGINE = InnoDB,
 PARTITION hour162 VALUES IN (162) ENGINE = InnoDB,
 PARTITION hour163 VALUES IN (163) ENGINE = InnoDB,
 PARTITION hour164 VALUES IN (164) ENGINE = InnoDB,
 PARTITION hour165 VALUES IN (165) ENGINE = InnoDB,
 PARTITION hour167 VALUES IN (167) ENGINE = InnoDB) */;
show create table mysql_ddl_test_t32;
drop table mysql_ddl_test_t32;
drop database mysql_ddl_test_db;