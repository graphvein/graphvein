-- Probe DDL of the Diesel oracle (T018g4, after the investigation's probe):
-- the forms neither crates.io nor the fixture's database has. The oracle
-- writes the schema files print-schema would write from this database
-- (TestDieselOracle's emulation of diesel_cli 2.3.14's rules) and compares
-- the claim pass over them with the catalog.
--
-- public: every builtin type of row DI-C2 and the types whose modifiers
-- print-schema drops (type families, with their arrays and the length
-- types without a length); lengths; arrays; an enum and its array; pg_catalog's
-- point (a sql_types struct with schema = "pg_catalog"); a type of another
-- schema (schema = "ext"); timetz, a pg_catalog type print-schema writes
-- as a struct (Revision 2, A2a); probe_vc, a custom type that takes a
-- type modifier (varchar's functions under another name, LANGUAGE
-- internal: a superuser-only stand-in for an extension type such as
-- pgvector's vector(1536); Revision 2, A2b); a type whose name format_type quotes; a domain
-- (DI-V3's failure case); Rust keywords, a name that is not an
-- identifier, a column named like its table and a generated column;
-- composite and missing primary keys; self, duplicate-pair, composite and
-- unique-target foreign keys; a view.
-- other: a schema = "other" section (a pub mod), a type of that schema, an
-- in-schema and a cross-schema key, and a renamed table (2.3.14 writes the
-- schema-qualified name into its #[sql_name]).
CREATE EXTENSION IF NOT EXISTS citext;
CREATE TYPE mood AS ENUM ('sad', 'ok');
CREATE TYPE "Shouty" AS ENUM ('A');
CREATE DOMAIN posint AS integer CHECK (VALUE > 0);
CREATE SCHEMA ext;
CREATE TYPE ext.color AS ENUM ('red', 'blue');
CREATE TYPE probe_vc;
CREATE FUNCTION probe_vc_in(cstring, oid, integer) RETURNS probe_vc AS 'varcharin' LANGUAGE internal IMMUTABLE STRICT;
CREATE FUNCTION probe_vc_out(probe_vc) RETURNS cstring AS 'varcharout' LANGUAGE internal IMMUTABLE STRICT;
CREATE FUNCTION probe_vc_typmod_in(cstring[]) RETURNS integer AS 'varchartypmodin' LANGUAGE internal IMMUTABLE STRICT;
CREATE FUNCTION probe_vc_typmod_out(integer) RETURNS cstring AS 'varchartypmodout' LANGUAGE internal IMMUTABLE STRICT;
CREATE TYPE probe_vc (INPUT = probe_vc_in, OUTPUT = probe_vc_out, TYPMOD_IN = probe_vc_typmod_in,
  TYPMOD_OUT = probe_vc_typmod_out, INTERNALLENGTH = VARIABLE, STORAGE = extended);

CREATE TABLE parents (id serial PRIMARY KEY, code varchar(10) UNIQUE NOT NULL);
CREATE TABLE kitchen (
  id bigserial PRIMARY KEY,
  i2 smallint NOT NULL, i4 integer, i8 bigint, f4 real, f8 double precision,
  n numeric, n2 numeric(10, 2), b boolean NOT NULL, t text, vc varchar, vc5 varchar(5),
  c3 char(3), ch "char", by bytea, d date, tm time, tm3 time(3), ts timestamp, ts3 timestamp(3),
  tstz timestamptz NOT NULL, tstz0 timestamptz(0), iv interval, iv2 interval day,
  j json, jb jsonb, u uuid, o oid, m money, ip inet, cd cidr, mac macaddr, mac8 macaddr8,
  bt bit(3), vb varbit, vb4 varbit(4), ci citext, x xml, tv tsvector, tq tsquery,
  r4 int4range, r8 int8range, rd daterange, rn numrange, rts tsrange, rtz tstzrange, lsn pg_lsn,
  ai integer[] NOT NULL, at2 text[][], av varchar(5)[], ab bool[], mo mood, mo_a mood[],
  bpc bpchar, bq "bit", ac char(2)[], abt bit(3)[], avb varbit(4)[], an numeric(5, 1)[],
  atm time[], ats timestamp(2)[], atz timestamptz[], aiv interval[],
  pt point, col ext.color, col_a ext.color[], sh "Shouty", dom posint,
  tz timetz, tz2 timetz(2), atz2 timetz(2)[], pv probe_vc(12), apv probe_vc(3)[],
  "type" text, "fn" integer NOT NULL, "Upper" int, "with space" int, "1st" int, kitchen text,
  gen int GENERATED ALWAYS AS (i4 * 2) STORED,
  parent_id integer REFERENCES parents (id) ON DELETE CASCADE,
  parent_code varchar(10) REFERENCES parents (code)
);
CREATE TABLE "type" (id integer PRIMARY KEY, kitchen_id bigint NOT NULL REFERENCES kitchen (id));
CREATE TABLE "Order Items" (order_id int, line int, qty int NOT NULL, PRIMARY KEY (order_id, line));
CREATE TABLE pair (a int, b int, PRIMARY KEY (a, b));
CREATE TABLE pair_child (id int PRIMARY KEY, a int, b int, FOREIGN KEY (a, b) REFERENCES pair (a, b));
CREATE TABLE selfref (id int PRIMARY KEY, parent int REFERENCES selfref (id));
CREATE TABLE twofk (id int PRIMARY KEY, p1 int REFERENCES parents (id), p2 int REFERENCES parents (id));
CREATE TABLE nopk (x int);
CREATE VIEW v_kitchen AS SELECT id, i4 FROM kitchen;

CREATE SCHEMA other;
CREATE TYPE other.state AS ENUM ('on', 'off');
CREATE TABLE other.items (id int PRIMARY KEY, parent_id int REFERENCES public.parents (id), label text NOT NULL, st other.state);
CREATE TABLE other.sub (id int PRIMARY KEY, item_id int NOT NULL REFERENCES other.items (id));
CREATE TABLE other."Mixed Up" (id int PRIMARY KEY, item_id int NOT NULL REFERENCES other.items (id));
