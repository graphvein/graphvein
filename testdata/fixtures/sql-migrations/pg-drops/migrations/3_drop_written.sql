-- A name the set wrote on a complete table (T018a2 C3, row M5g-1): the
-- unique key on email is dropped exactly; the primary key that projects
-- references stays, so the migration is applied.
ALTER TABLE accounts DROP CONSTRAINT accounts_email_key;
