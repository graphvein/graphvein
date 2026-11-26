-- One run of the Diesel oracle (scripts/oracle/diesel_catalog.sh) as a JSON
-- object: the psql variables name, kind, repo, sets, applied, failed and
-- failures, the catalog the claims are compared with, and the
-- information_schema rows diesel_cli's print-schema reads (2.3.14
-- infer_schema_internals/information_schema.rs and pg.rs), from which
-- TestDieselOracle emulates print-schema for the probe run.
SELECT json_build_object(
  'name', :'name',
  'kind', :'kind',
  'repo', :'repo',
  'sets', CASE WHEN :'sets' = '' THEN '[]'::json ELSE to_json(string_to_array(:'sets', ',')) END,
  'migrations', json_build_object('applied', :'applied'::int, 'failed', :'failed'::int,
    'failures', to_json(array_remove(string_to_array(:'failures', E'\n'), ''))),
  'default_schema', current_schema(),
  'catalog', json_build_object(
    'relations', COALESCE((SELECT json_agg(json_build_object('schema', n.nspname, 'name', c.relname, 'relkind', c.relkind) ORDER BY n.nspname, c.relname)
      FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f') AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%'), '[]'::json),
    'columns', COALESCE((SELECT json_agg(json_build_object(
        'schema', n.nspname, 'table', c.relname, 'name', a.attname, 'num', a.attnum,
        'type', format_type(a.atttypid, a.atttypmod), 'base', format_type(a.atttypid, NULL),
        'typname', t.typname, 'typschema', tn.nspname, 'typtype', t.typtype,
        'elemname', et.typname, 'elemschema', etn.nspname, 'notnull', a.attnotnull) ORDER BY n.nspname, c.relname, a.attnum)
      FROM pg_attribute a
      JOIN pg_class c ON c.oid = a.attrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_type t ON t.oid = a.atttypid
      JOIN pg_namespace tn ON tn.oid = t.typnamespace
      LEFT JOIN pg_type et ON et.oid = t.typelem AND t.typcategory = 'A'
      LEFT JOIN pg_namespace etn ON etn.oid = et.typnamespace
      WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f') AND a.attnum > 0 AND NOT a.attisdropped
        AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%'), '[]'::json),
    'constraints', COALESCE((SELECT json_agg(json_build_object(
        'schema', n.nspname, 'table', c.relname, 'name', k.conname, 'type', k.contype,
        'columns', (SELECT array_agg(a.attname::text ORDER BY u.o) FROM unnest(k.conkey) WITH ORDINALITY u(num, o) JOIN pg_attribute a ON a.attrelid = k.conrelid AND a.attnum = u.num),
        'ref_schema', rn.nspname, 'ref_table', rc.relname,
        'ref_columns', (SELECT array_agg(a.attname::text ORDER BY u.o) FROM unnest(k.confkey) WITH ORDINALITY u(num, o) JOIN pg_attribute a ON a.attrelid = k.confrelid AND a.attnum = u.num)) ORDER BY n.nspname, c.relname, k.conname)
      FROM pg_constraint k
      JOIN pg_class c ON c.oid = k.conrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      LEFT JOIN pg_class rc ON rc.oid = k.confrelid
      LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace
      WHERE k.contype IN ('p', 'u', 'f') AND n.nspname NOT IN ('pg_catalog', 'information_schema')), '[]'::json)
  ),
  'info', json_build_object(
    'tables', COALESCE((SELECT json_agg(json_build_object('table_schema', table_schema, 'table_name', table_name, 'table_type', table_type) ORDER BY table_schema, table_name)
      FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema')), '[]'::json),
    'columns', COALESCE((SELECT json_agg(json_build_object('table_schema', table_schema, 'table_name', table_name, 'column_name', column_name,
        'ordinal_position', ordinal_position, 'udt_name', udt_name, 'udt_schema', udt_schema, 'is_nullable', is_nullable,
        'character_maximum_length', character_maximum_length, 'domain_name', domain_name) ORDER BY table_schema, table_name, ordinal_position)
      FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog', 'information_schema')), '[]'::json),
    'primary_keys', COALESCE((SELECT json_agg(json_build_object('table_schema', n.nspname, 'table_name', c.relname, 'column_name', a.attname) ORDER BY n.nspname, c.relname, u.o)
      FROM pg_constraint k
      JOIN pg_class c ON c.oid = k.conrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace,
      unnest(k.conkey) WITH ORDINALITY u(num, o)
      JOIN pg_attribute a ON a.attnum = u.num
      WHERE a.attrelid = k.conrelid AND k.contype = 'p' AND n.nspname NOT IN ('pg_catalog', 'information_schema')), '[]'::json)
  )
)
