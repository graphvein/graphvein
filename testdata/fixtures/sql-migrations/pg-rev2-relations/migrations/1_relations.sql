-- WF3: relations share one namespace per schema, so CREATE TABLE IF NOT
-- EXISTS of a view, sequence, index, composite type, materialized view,
-- SELECT INTO target or CREATE SCHEMA element is skipped
-- (parse_utilcmd.c 195-211): no table is created.
CREATE TABLE base (id integer PRIMARY KEY);
CREATE TABLE dropped_later (id integer);
CREATE VIEW base_view AS SELECT id FROM base;
CREATE TABLE IF NOT EXISTS base_view (id integer, extra text);
CREATE SEQUENCE counter_seq;
CREATE TABLE IF NOT EXISTS counter_seq (n integer);
CREATE INDEX base_lookup ON base (id);
CREATE TABLE IF NOT EXISTS base_lookup (n integer);
CREATE TYPE pair AS (a integer, b integer);
CREATE TABLE IF NOT EXISTS pair (a integer);
CREATE MATERIALIZED VIEW base_totals AS SELECT count(*) AS n FROM base;
CREATE TABLE IF NOT EXISTS base_totals (n bigint);
SELECT 1 AS one INTO selected;
CREATE TABLE IF NOT EXISTS selected (one integer, two integer);
CREATE SCHEMA reports CREATE TABLE summary (id integer) CREATE VIEW summary_view AS SELECT 1;
CREATE TABLE IF NOT EXISTS reports.summary (id integer, total numeric);
