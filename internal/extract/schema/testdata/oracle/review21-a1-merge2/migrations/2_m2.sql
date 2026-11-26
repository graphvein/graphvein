MERGE INTO t USING (VALUES (1, 5)) AS s(id, v) ON t.id = s.id WHEN MATCHED THEN UPDATE SET v = s.v;
