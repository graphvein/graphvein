CREATE TYPE status AS ENUM ('a', 'b');
CREATE TABLE orders (id int PRIMARY KEY, status status NOT NULL);
SELECT * INTO orders_copy FROM orders;
DROP TABLE orders;
DROP TYPE status;
