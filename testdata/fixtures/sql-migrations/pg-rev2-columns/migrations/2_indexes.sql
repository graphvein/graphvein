-- G5: a DEFAULT that calls a function the set defines runs it on the
-- existing rows: the statement is modeled and also counts as opaque.
CREATE FUNCTION pick_default() RETURNS text LANGUAGE sql AS $$ SELECT 'x' $$;
ALTER TABLE handles ADD COLUMN picked text DEFAULT pick_default();

-- WF5: a schema-qualified DROP INDEX may name the index of an unqualified
-- table; the index is kept and the table is partial. IF EXISTS keeps the
-- migration applied: without it the name may mean no index (Revision 8).
CREATE TABLE ledger (id integer PRIMARY KEY, memo text);
CREATE INDEX ledger_memo_idx ON ledger (memo);
DROP INDEX IF EXISTS public.ledger_memo_idx;

-- WF5: ALTER INDEX ... RENAME accepts any relation (tablecmds.c
-- 17901-17906, 17931-17937), so it renames the table journal.
CREATE TABLE journal (id integer);
ALTER INDEX journal RENAME TO journal_v2;
CREATE INDEX journal_id_idx ON journal_v2 (id);
ALTER INDEX journal_id_idx RENAME TO journal_v2_id_idx;
