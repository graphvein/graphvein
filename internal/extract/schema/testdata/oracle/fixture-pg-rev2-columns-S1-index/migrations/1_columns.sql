CREATE TABLE ledger (memo text);
CREATE INDEX ledger_memo_idx ON ledger (memo);
DROP INDEX IF EXISTS public.ledger_memo_idx;
