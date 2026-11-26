-- R1: ADD COLUMN takes the same serial path as CREATE TABLE.
ALTER TABLE counters ADD COLUMN j bigserial;
ALTER TABLE counters ADD k smallint;
