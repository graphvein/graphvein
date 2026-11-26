-- WF1: EXCLUDE is an unreserved keyword (gram.y 17616), so an unquoted
-- column may be named exclude; only EXCLUDE [USING m] ( starts a constraint.
CREATE TABLE bookings (
    id integer PRIMARY KEY,
    exclude boolean NOT NULL,
    during tstzrange,
    code text UNIQUE,
    CONSTRAINT bookings_no_overlap EXCLUDE USING gist (during WITH &&)
);
CREATE TABLE flags (id integer);
ALTER TABLE flags ADD exclude text;

-- G3: unique is true for a single-column primary key, UNIQUE constraint or
-- unique index without predicate or expression, false otherwise on a
-- complete table.
CREATE TABLE handles (
    id integer PRIMARY KEY,
    nick text,
    email text,
    partial_key text,
    pair_a integer,
    pair_b integer,
    plain text
);
CREATE UNIQUE INDEX handles_nick_key ON handles (nick);
CREATE UNIQUE INDEX handles_lower_email ON handles (lower(email));
CREATE UNIQUE INDEX handles_partial ON handles (partial_key) WHERE partial_key IS NOT NULL;
CREATE UNIQUE INDEX handles_pair ON handles (pair_a, pair_b);

-- WF7: DROP COLUMN ... CASCADE also drops the generated columns computed
-- from it (pg_attrdef.c 187-195), their indexes, and the CHECK constraints
-- that use the column.
CREATE TABLE prices (
    id integer PRIMARY KEY,
    net numeric NOT NULL,
    gross numeric GENERATED ALWAYS AS (net * 1.2) STORED,
    label text,
    CONSTRAINT prices_net_check CHECK (net > 0)
);
CREATE INDEX prices_gross_idx ON prices (gross);
ALTER TABLE prices DROP COLUMN net CASCADE;
