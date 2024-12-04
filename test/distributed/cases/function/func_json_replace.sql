CREATE TABLE users (
    id INT PRIMARY KEY,
    info JSON
);

INSERT INTO users (id, info) VALUES
(1, '{"name": "Alice", "age": 30, "email": "alice@example.com", "address": {"city": "New York", "zip": "10001"}}'),
(2, '{"name": "Bob", "age": 25, "email": "bob@example.com", "address": {"city": "Los Angeles", "zip": "90001"}}'),
(3, '{"name": "Charlie", "age": 28, "email": "charlie@example.com", "address": {"city": "Chicago", "zip": "60601"}, "skills": ["Java", "Python"]}');

SELECT * FROM users;

UPDATE users
SET info = JSON_REPLACE(info, '$.age', 31)
WHERE id = 1;
SELECT * FROM users;

UPDATE users
SET info = JSON_REPLACE(info, '$.address.city', 'San Francisco')
WHERE id = 1;
SELECT * FROM users;

UPDATE users
SET info = JSON_REPLACE(info, '$.skills[0]', 'JavaScript')
WHERE id = 3;
SELECT * FROM users;

UPDATE users
SET info = JSON_REPLACE(info, '$.age', 32, '$.address.city', 'San Francisco')
WHERE id = 1;
SELECT * FROM users;

drop table users;
