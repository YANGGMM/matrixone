CREATE DATABASE exampledb;
USE exampledb;

CREATE TABLE example_table (
    id INT AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    data VARCHAR(255) NOT NULL
);

INSERT INTO example_table (data) VALUES ('Record 1'),('Record 2'),('Record 3'),('Record 4'),('Record 5'),('Record 6'),('Record 7'),('Record 8'),('Record 9'),('Record 10');

-- @ignore:1
SELECT * FROM example_table LIMIT 10;

-- @ignore:1
SELECT id, now() FROM example_table LIMIT 10;

drop database exampledb;
