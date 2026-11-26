-- WF8: after a migration that may fail, sqlx may never run this one.
CREATE TABLE after_refused (id integer);
