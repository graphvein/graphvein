CREATE TABLE p ("order" int PRIMARY KEY);
CREATE TABLE c (x int REFERENCES p (order));
