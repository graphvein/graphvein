CREATE COLLATION c1 (provider = icu, locale = 'und'); CREATE TABLE t (a text COLLATE c1);
