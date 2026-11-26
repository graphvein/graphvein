/* lowercase keywords; this comment /* nests; */ and still continues; */
create table accounts (
    account_id serial primary key,
    email text not null constraint accounts_email_key unique,
    "DisplayName" varchar(80) collate "C" default E'it\'s; fine',
    status text not null default 'active' check (status in ('active', 'closed')),
    created_at timestamptz not null default now()
);

create function account_count() returns bigint language sql as $body$
    select 1::bigint; -- a semicolon inside the dollar-quoted body
$body$;

create table scratch (id integer, old text);

CREATE TABLE "MixedCase" (
    "Id" BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    Label TEXT NULL,
    doubled integer GENERATED ALWAYS AS (length(label) * 2) STORED
);

CREATE TABLE Seventy_Byte_Unquoted_Table_Name_That_Postgres_Truncates_To_63_xyzabcd (id integer);
