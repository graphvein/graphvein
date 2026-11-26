-- The database the fixture's Postgres schema files describe
-- (testdata/fixtures/diesel-models/pg/src: schema.rs before its patch,
-- audit.rs, filtered.rs, broken.rs, nohead.rs, v24.rs, patched.rs before
-- its patch; and pg20/src/schema.rs, diesel 2.0), for the Diesel oracle's
-- fixture run (T018g4). Each generated line is what print-schema
-- would write for these tables; lines the fixture writes by hand (the
-- patch's lines, nohead.rs, broken.rs, D and DI-P1 forms) claim names
-- only, and the tables they name exist here so the names agree.
CREATE EXTENSION IF NOT EXISTS citext;
CREATE SCHEMA ext;
CREATE EXTENSION IF NOT EXISTS ltree SCHEMA ext;
CREATE TYPE mood AS ENUM ('sad', 'ok');

CREATE TABLE scalars (
  id integer PRIMARY KEY, small smallint NOT NULL, big bigint, f4 real NOT NULL, f8 double precision NOT NULL,
  flag boolean NOT NULL, body text NOT NULL, vc varchar NOT NULL, vc5 varchar(5) NOT NULL, c3 char(3) NOT NULL,
  c char(2) NOT NULL, ch "char" NOT NULL, raw bytea NOT NULL, day date NOT NULL, at time NOT NULL,
  ts timestamp NOT NULL, tstz timestamptz NOT NULL, span interval NOT NULL, amount numeric(12, 2) NOT NULL,
  j json NOT NULL, jb jsonb NOT NULL, u uuid NOT NULL, o oid NOT NULL, m money NOT NULL, ip inet NOT NULL,
  net cidr NOT NULL, mac macaddr NOT NULL, mac8 macaddr8 NOT NULL, ci citext NOT NULL, tv tsvector NOT NULL,
  tq tsquery NOT NULL, bits bit(3) NOT NULL, bit1 bit(5) NOT NULL, vb varbit NOT NULL, vb8 varbit(8) NOT NULL,
  x xml NOT NULL, r4 int4range NOT NULL, r8 int8range NOT NULL, dr daterange NOT NULL, nr numrange NOT NULL,
  tr tsrange NOT NULL, tzr tstzrange NOT NULL, lsn pg_lsn NOT NULL, tags text[] NOT NULL, nums integer[],
  names varchar(20)[] NOT NULL, mood mood NOT NULL, moods mood[] NOT NULL, path ext.ltree NOT NULL,
  pt point NOT NULL, cie citext NOT NULL, lite text NOT NULL, rng int4range NOT NULL, q text NOT NULL,
  bogus text NOT NULL, qn integer, "Weird Name" text NOT NULL, "type" text NOT NULL, badlen integer NOT NULL
);
CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL, email text);
CREATE TABLE posts (id integer PRIMARY KEY, author_id integer NOT NULL REFERENCES users (id), editor_id integer, title text NOT NULL);
CREATE TABLE pairs (a integer, b integer, user_id integer NOT NULL REFERENCES users (id), PRIMARY KEY (a, b));
CREATE TABLE nokey (id integer NOT NULL, val text NOT NULL);
CREATE TABLE order_lines (id integer PRIMARY KEY, post_id integer NOT NULL REFERENCES posts (id));
CREATE TABLE legacy (id integer PRIMARY KEY REFERENCES users (id), gone text NOT NULL, kept text NOT NULL);
CREATE TABLE tags (id integer PRIMARY KEY, label text NOT NULL, draft boolean NOT NULL DEFAULT false);
CREATE TABLE notes (id integer PRIMARY KEY, post_id integer NOT NULL REFERENCES posts (id));
CREATE TABLE "match" (id integer PRIMARY KEY);
CREATE TABLE odd (id integer PRIMARY KEY);
CREATE TABLE broken_key (id integer PRIMARY KEY);
CREATE TABLE extras (id integer PRIMARY KEY, attr text NOT NULL);
CREATE VIEW view_counts AS SELECT post_id, count(*) AS n FROM notes GROUP BY post_id;

-- filtered.rs, broken.rs, nohead.rs.
CREATE TABLE kept (id integer PRIMARY KEY);
CREATE TABLE accounts (id integer PRIMARY KEY, balance bigint NOT NULL);
CREATE TABLE handmade (id integer PRIMARY KEY, note text);

-- audit.rs ([print_schema.audit] schema = "audit").
CREATE SCHEMA audit;
CREATE TYPE audit.level AS ENUM ('info', 'warn');
CREATE TABLE audit.actors (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE audit.events (id bigint PRIMARY KEY, actor_id bigint NOT NULL REFERENCES audit.actors (id), level audit.level NOT NULL, search tsvector NOT NULL);
CREATE TABLE audit."Old Names" (id bigint PRIMARY KEY);

-- v24.rs (diesel_cli 2.4 output, Revision 2 A4 and A2): an enum, an enum
-- of another schema, timetz and a composite type.
CREATE TYPE shape AS (x integer, y integer);
CREATE TABLE people24 (id integer PRIMARY KEY, mood mood NOT NULL, moods mood[] NOT NULL, level audit.level,
  opens timetz(3) NOT NULL, opens_all timetz[] NOT NULL, outline shape);

-- patched.rs (Revision 2, A3): print-schema wrote name -> Varchar and the
-- struct of user_kind; the patch adds #[max_length = 10] and renames the
-- struct's type to my_kind, neither of which the database has.
CREATE TYPE user_kind AS ENUM ('a', 'b');
CREATE TABLE patched_users (id integer PRIMARY KEY, name varchar NOT NULL, kind user_kind NOT NULL, note text);

-- pg20/src/schema.rs (diesel 2.0, Revision 2 A1): diesel_cli 2.0 drops
-- the lengths, so varchar(40) is written Varchar.
CREATE TABLE old_users (id integer PRIMARY KEY, name varchar(40) NOT NULL, bits varbit(8) NOT NULL, tags varchar(10)[] NOT NULL, body text NOT NULL);
CREATE TABLE old_posts (id integer PRIMARY KEY, user_id integer NOT NULL REFERENCES old_users (id));
