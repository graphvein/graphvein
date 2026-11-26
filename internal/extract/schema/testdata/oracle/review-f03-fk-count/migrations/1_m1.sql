CREATE TABLE p (a int, b int, PRIMARY KEY (a, b)); CREATE TABLE c (x int REFERENCES p (a, b));
