-- R5: the test adds 2_unreadable.sql (invalid UTF-8) and, where the platform
-- allows, a symlink 4_linked.sql in its temporary copy. Neither is committed.
CREATE TABLE first_table (id integer);
