-- WF8: after a migration that may fail, sqlx may never run this one.
CREATE TABLE after_fail (id integer);
DROP TABLE dropped_later;
