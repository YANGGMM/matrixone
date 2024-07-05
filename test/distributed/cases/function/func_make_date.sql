SELECT MAKEDATE(0, 0);
SELECT MAKEDATE(2024, 1); 
SELECT MAKEDATE(2024, 366);
SELECT DATEDIFF(MAKEDATE(2024, 1), '2024-07-01');
-- @ignore:0
SELECT MAKEDATE(YEAR(NOW()), DAY(LAST_DAY(NOW())));
-- @ignore:0
SELECT DATE_SUB(MAKEDATE(YEAR(CURDATE()), 1), INTERVAL 1 DAY) AS LastDayOfYear;

-- @bvt:issue#3626
SELECT '生日快乐！' AS message, MAKEDATE(1990, 100) AS birthday; 
SELECT
    YEAR(NOW()) - YEAR(MAKEDATE(YEAR(NOW()), 1)) AS years_passed,
    DAY(NOW()) AS days_into_year
FROM DUAL;
-- @bvt:issue

SELECT IF(MAKEDATE(YEAR(NOW()), 366) < NOW(), '闰年', '平年') AS leap_year_status;

create database abc;
use abc;
CREATE TABLE employees (
    id INT AUTO_INCREMENT PRIMARY KEY,
    employee_name VARCHAR(255) NOT NULL,
    employee_hire_date DATE,
    company_founded DATE,
    employee_status ENUM('active', 'inactive') NOT NULL
);

INSERT INTO employees (employee_name, employee_hire_date, company_founded, employee_status)
VALUES
    ('Alice Smith', '2020-06-01', '2000-01-01', 'active'),
    ('Bob Johnson', '1999-12-15', '2000-01-01', 'inactive'),
    ('Charlie Brown', '2022-03-20', '2000-01-01', 'active'),
    ('Diana Prince', '2019-11-30', '1998-12-31', 'active');

SELECT employee_hire_date,
       MAKEDATE(YEAR(company_founded), 1) AS CompanyFoundedDate,
       CASE 
           WHEN employee_hire_date > MAKEDATE(YEAR(company_founded), 1) THEN 'After Foundation'
           ELSE 'Before Foundation'
       END AS HirePeriod
FROM employees
WHERE employee_status = 'active';

drop database abc;

SELECT MAKEDATE(2011,31), MAKEDATE(2011,32);
SELECT MAKEDATE(2011,365), MAKEDATE(2014,365);
SELECT MAKEDATE(2011,0);
SELECT MAKEDATE(2017, 3);
SELECT MAKEDATE(2017, 175);
SELECT MAKEDATE(2017, 100);
SELECT MAKEDATE(2017, 366);
