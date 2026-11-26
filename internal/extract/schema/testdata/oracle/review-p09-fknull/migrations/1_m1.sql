CREATE TABLE p (a int, b int, PRIMARY KEY (a, b));
CREATE TABLE c (x int, y int, FOREIGN KEY (x, y) REFERENCES p (a, b));
INSERT INTO c (x, y) VALUES (1, NULL);
CREATE TABLE c2 (x int, y int, FOREIGN KEY (x, y) REFERENCES p (a, b) MATCH FULL);
INSERT INTO c2 (x, y) VALUES (1, NULL);
