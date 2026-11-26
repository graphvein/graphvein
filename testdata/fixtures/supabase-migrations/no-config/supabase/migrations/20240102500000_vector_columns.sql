-- NX-F2 (SB-M10): types the platform extensions created in `extensions`
-- (SB-M6) are column types, unqualified through the platform's
-- search_path ("$user", public, extensions) or qualified; vector and
-- halfvec take one dimension from 1 to 16000.
create table embeddings (
  id integer primary key,
  embedding vector(3),
  half extensions.halfvec(16000),
  headers http_header[]
);
