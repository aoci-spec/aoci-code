package dbevidence

const (
	postgresFactsSQL  = `SELECT current_setting('server_version')`
	postgresTablesSQL = `
SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
ORDER BY table_schema, table_name`
	postgresColumnsSQL = `
SELECT c.table_schema, c.table_name, c.ordinal_position, c.column_name,
       pg_catalog.format_type(a.atttypid, a.atttypmod), c.data_type,
       c.is_nullable = 'YES', c.column_default,
       EXISTS (
         SELECT 1
         FROM pg_catalog.pg_depend AS d
         JOIN pg_catalog.pg_class AS seq ON seq.oid = d.objid AND seq.relkind = 'S'
         WHERE d.refobjid = t.oid AND d.refobjsubid = a.attnum AND d.deptype = 'a'
       ),
       c.is_identity, c.identity_generation, c.is_generated, c.generation_expression
FROM information_schema.columns AS c
JOIN pg_catalog.pg_namespace AS n ON n.nspname = c.table_schema
JOIN pg_catalog.pg_class AS t ON t.relnamespace = n.oid AND t.relname = c.table_name
JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid AND a.attname = c.column_name AND NOT a.attisdropped
WHERE t.relkind IN ('r', 'p')
ORDER BY c.table_schema, c.table_name, c.ordinal_position`
	postgresKeysSQL = `
SELECT n.nspname, t.relname, c.conname, c.contype::text, k.ordinality, a.attname
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, ordinality)
JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid AND a.attnum = k.attnum
WHERE c.contype IN ('p', 'u')
ORDER BY n.nspname, t.relname, c.conname, k.ordinality`
	postgresForeignKeysSQL = `
SELECT n.nspname, t.relname, c.conname, s.ordinality, sa.attname,
       rn.nspname, rt.relname, ra.attname,
       CASE c.confupdtype WHEN 'a' THEN 'no action' WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade' WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default' END,
       CASE c.confdeltype WHEN 'a' THEN 'no action' WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade' WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default' END
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_class AS rt ON rt.oid = c.confrelid
JOIN pg_catalog.pg_namespace AS rn ON rn.oid = rt.relnamespace
CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS s(attnum, ordinality)
CROSS JOIN LATERAL unnest(c.confkey) WITH ORDINALITY AS d(attnum, ordinality)
JOIN pg_catalog.pg_attribute AS sa ON sa.attrelid = t.oid AND sa.attnum = s.attnum
JOIN pg_catalog.pg_attribute AS ra ON ra.attrelid = rt.oid AND ra.attnum = d.attnum
WHERE c.contype = 'f' AND s.ordinality = d.ordinality
ORDER BY n.nspname, t.relname, c.conname, s.ordinality`
	postgresChecksSQL = `
SELECT n.nspname, t.relname, c.conname, pg_catalog.pg_get_constraintdef(c.oid, true)
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
WHERE c.contype = 'c'
ORDER BY n.nspname, t.relname, c.conname`
	postgresIndexesSQL = `
SELECT n.nspname, t.relname, i.relname, x.indisunique, x.indisprimary,
       am.amname, p.position, a.attname,
       CASE WHEN a.attname IS NULL THEN pg_catalog.pg_get_indexdef(i.oid, p.position, true) END,
       p.position > x.indnkeyatts, COALESCE(pg_catalog.pg_get_expr(x.indpred, x.indrelid, true), '')
FROM pg_catalog.pg_index AS x
JOIN pg_catalog.pg_class AS t ON t.oid = x.indrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_class AS i ON i.oid = x.indexrelid
JOIN pg_catalog.pg_am AS am ON am.oid = i.relam
CROSS JOIN LATERAL generate_series(1, x.indnatts) AS p(position)
LEFT JOIN pg_catalog.pg_attribute AS a
  ON a.attrelid = t.oid AND a.attnum = x.indkey[p.position - 1] AND x.indkey[p.position - 1] > 0
ORDER BY n.nspname, t.relname, i.relname, p.position`
	postgresPartitionParentsSQL = `
SELECT n.nspname, t.relname,
       CASE p.partstrat WHEN 'r' THEN 'range' WHEN 'l' THEN 'list' WHEN 'h' THEN 'hash' END,
       pg_catalog.pg_get_partkeydef(t.oid)
FROM pg_catalog.pg_partitioned_table AS p
JOIN pg_catalog.pg_class AS t ON t.oid = p.partrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
ORDER BY n.nspname, t.relname`
	postgresPartitionChildrenSQL = `
SELECT cn.nspname, child.relname, pn.nspname, parent.relname,
       pg_catalog.pg_get_expr(child.relpartbound, child.oid, true)
FROM pg_catalog.pg_inherits AS inh
JOIN pg_catalog.pg_class AS child ON child.oid = inh.inhrelid
JOIN pg_catalog.pg_namespace AS cn ON cn.oid = child.relnamespace
JOIN pg_catalog.pg_class AS parent ON parent.oid = inh.inhparent
JOIN pg_catalog.pg_namespace AS pn ON pn.oid = parent.relnamespace
WHERE child.relispartition AND child.relkind IN ('r', 'p')
ORDER BY cn.nspname, child.relname`

	mysqlFactsSQL  = `SELECT VERSION(), @@lower_case_table_names`
	mysqlTablesSQL = `
SELECT TABLE_SCHEMA, TABLE_NAME
FROM information_schema.TABLES
WHERE TABLE_TYPE = 'BASE TABLE'
ORDER BY TABLE_SCHEMA, TABLE_NAME`
	mysqlColumnsSQL = `
SELECT TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION, COLUMN_NAME,
       COLUMN_TYPE, DATA_TYPE, IS_NULLABLE = 'YES', COLUMN_DEFAULT,
       EXTRA, GENERATION_EXPRESSION
FROM information_schema.COLUMNS
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`
	mysqlKeysSQL = `
SELECT k.TABLE_SCHEMA, k.TABLE_NAME, k.CONSTRAINT_NAME, t.CONSTRAINT_TYPE,
       k.ORDINAL_POSITION, k.COLUMN_NAME
FROM information_schema.KEY_COLUMN_USAGE AS k
JOIN information_schema.TABLE_CONSTRAINTS AS t
  ON t.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
 AND t.TABLE_SCHEMA = k.TABLE_SCHEMA
 AND t.TABLE_NAME = k.TABLE_NAME
 AND t.CONSTRAINT_NAME = k.CONSTRAINT_NAME
WHERE t.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE')
ORDER BY k.TABLE_SCHEMA, k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION`
	mysqlForeignKeysSQL = `
SELECT k.TABLE_SCHEMA, k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION,
       k.COLUMN_NAME, k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME,
       k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE
FROM information_schema.KEY_COLUMN_USAGE AS k
JOIN information_schema.REFERENTIAL_CONSTRAINTS AS r
  ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
 AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME
 AND r.TABLE_NAME = k.TABLE_NAME
WHERE k.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY k.TABLE_SCHEMA, k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION`
	mysqlChecksSQL = `
SELECT t.TABLE_SCHEMA, t.TABLE_NAME, t.CONSTRAINT_NAME, c.CHECK_CLAUSE
FROM information_schema.TABLE_CONSTRAINTS AS t
JOIN information_schema.CHECK_CONSTRAINTS AS c
  ON c.CONSTRAINT_SCHEMA = t.CONSTRAINT_SCHEMA
 AND c.CONSTRAINT_NAME = t.CONSTRAINT_NAME
WHERE t.CONSTRAINT_TYPE = 'CHECK'
ORDER BY t.TABLE_SCHEMA, t.TABLE_NAME, t.CONSTRAINT_NAME`
	mysqlIndexesSQL = `
SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX,
       COLUMN_NAME, EXPRESSION, SUB_PART, COLLATION, INDEX_TYPE, IS_VISIBLE
FROM information_schema.STATISTICS
ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`
	mysqlPartitionsSQL = `
SELECT TABLE_SCHEMA, TABLE_NAME, PARTITION_NAME, PARTITION_ORDINAL_POSITION,
       PARTITION_METHOD, PARTITION_EXPRESSION, PARTITION_DESCRIPTION
FROM information_schema.PARTITIONS
WHERE PARTITION_NAME IS NOT NULL
ORDER BY TABLE_SCHEMA, TABLE_NAME, PARTITION_ORDINAL_POSITION`

	// openGauss has its own catalog contract even where individual columns
	// resemble PostgreSQL. These queries intentionally do not alias the
	// PostgreSQL inventory. The v1 adapter supports openGauss 6.0.5 LTS in A
	// or PG compatibility mode and ordinary, permanent, row-store base tables.
	openGaussFactsSQL = `
SELECT pg_catalog.opengauss_version(),
       current_setting('server_version'),
       pg_catalog.gs_deployment(),
       d.datcompatibility::text,
       current_database()
FROM pg_catalog.pg_database AS d
WHERE d.datname = current_database()`
	openGaussTablesSQL = `
SELECT n.nspname, c.relname
FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
  AND c.relpersistence = 'p'
  AND c.parttype = 'n'
  AND c.reldeltarelid = 0
  AND c.relcudescrelid = 0
  AND LOWER(COALESCE(c.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(c.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(c.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
  AND NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_inherits AS h
    WHERE h.inhrelid = c.oid OR h.inhparent = c.oid
  )
  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
  AND (pg_catalog.pg_has_role(c.relowner, 'USAGE')
       OR pg_catalog.has_table_privilege(c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
       OR pg_catalog.has_any_column_privilege(c.oid, 'SELECT, INSERT, UPDATE, REFERENCES'))
ORDER BY n.nspname, c.relname`
	openGaussUnsupportedSQL = `
SELECT n.nspname, c.relname,
       CASE
         WHEN n.nspblockchain THEN 'blockchain_schema'
         WHEN c.relkind = 'f' THEN 'foreign_table'
         WHEN c.relkind = 'v' THEN 'view'
         WHEN c.relkind = 'm' THEN 'materialized_view'
         WHEN c.parttype <> 'n' THEN 'partitioned_table'
         WHEN c.relpersistence <> 'p' THEN 'non_permanent_table'
         WHEN c.reldeltarelid <> 0 OR c.relcudescrelid <> 0 THEN 'column_store_table'
         WHEN LOWER(COALESCE(c.reloptions::text, '')) LIKE '%orientation=column%' THEN 'column_store_table'
         WHEN LOWER(COALESCE(c.reloptions::text, '')) LIKE '%storage_type=ustore%' THEN 'ustore_table'
         WHEN LOWER(COALESCE(c.reloptions::text, '')) LIKE '%storage_type=mot%' THEN 'mot_table'
         WHEN EXISTS (
           SELECT 1 FROM pg_catalog.pg_inherits AS h
           WHERE h.inhrelid = c.oid OR h.inhparent = c.oid
         ) THEN 'table_inheritance'
         WHEN EXISTS (
           SELECT 1 FROM pg_catalog.pg_constraint AS k
           WHERE k.conrelid = c.oid AND k.contype NOT IN ('p', 'u', 'f', 'c')
         ) THEN 'constraint_type'
         WHEN EXISTS (
           SELECT 1 FROM pg_catalog.pg_constraint AS k
           WHERE k.conrelid = c.oid
             AND (k.condeferrable OR k.condeferred OR NOT k.convalidated
                  OR (k.contype = 'f' AND k.confmatchtype <> 'u')
                  OR k.condisable OR k.consoft OR k.conopt
                  OR COALESCE(pg_catalog.array_length(k.conincluding, 1), 0) <> 0
                  OR (k.contype = 'c' AND k.connoinherit)
                  OR NOT k.conislocal OR k.coninhcount <> 0)
         ) THEN 'constraint_state'
         WHEN EXISTS (
           SELECT 1
           FROM pg_catalog.pg_index AS x
           JOIN pg_catalog.pg_class AS i ON i.oid = x.indexrelid
           WHERE x.indrelid = c.oid AND i.relkind = 'I'
         ) THEN 'global_partition_index'
         WHEN EXISTS (
           SELECT 1
           FROM pg_catalog.pg_partition AS p
           WHERE p.parentid = c.oid AND p.parttype IN ('r', 'p', 's', 'x')
         ) THEN 'pg_partition'
       END
FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'f', 'v', 'm')
  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
  AND (pg_catalog.pg_has_role(c.relowner, 'USAGE')
       OR pg_catalog.has_table_privilege(c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
       OR pg_catalog.has_any_column_privilege(c.oid, 'SELECT, INSERT, UPDATE, REFERENCES'))
  AND (n.nspblockchain
       OR c.relkind = 'f'
       OR c.relkind IN ('v', 'm')
       OR c.parttype <> 'n'
       OR c.relpersistence <> 'p'
       OR c.reldeltarelid <> 0
       OR c.relcudescrelid <> 0
       OR LOWER(COALESCE(c.reloptions::text, '')) LIKE '%orientation=column%'
       OR LOWER(COALESCE(c.reloptions::text, '')) LIKE '%storage_type=ustore%'
       OR LOWER(COALESCE(c.reloptions::text, '')) LIKE '%storage_type=mot%'
       OR EXISTS (
         SELECT 1 FROM pg_catalog.pg_inherits AS h
         WHERE h.inhrelid = c.oid OR h.inhparent = c.oid
       )
       OR EXISTS (
         SELECT 1 FROM pg_catalog.pg_constraint AS k
         WHERE k.conrelid = c.oid AND k.contype NOT IN ('p', 'u', 'f', 'c')
       )
       OR EXISTS (
         SELECT 1 FROM pg_catalog.pg_constraint AS k
         WHERE k.conrelid = c.oid
           AND (k.condeferrable OR k.condeferred OR NOT k.convalidated
                OR (k.contype = 'f' AND k.confmatchtype <> 'u')
                OR k.condisable OR k.consoft OR k.conopt
                OR COALESCE(pg_catalog.array_length(k.conincluding, 1), 0) <> 0
                OR (k.contype = 'c' AND k.connoinherit)
                OR NOT k.conislocal OR k.coninhcount <> 0)
       )
       OR EXISTS (
         SELECT 1
         FROM pg_catalog.pg_index AS x
         JOIN pg_catalog.pg_class AS i ON i.oid = x.indexrelid
         WHERE x.indrelid = c.oid AND i.relkind = 'I'
       )
       OR EXISTS (
         SELECT 1
         FROM pg_catalog.pg_partition AS p
         WHERE p.parentid = c.oid AND p.parttype IN ('r', 'p', 's', 'x')
       ))
ORDER BY n.nspname, c.relname`
	openGaussPartitionsSQL = `
SELECT n.nspname, c.relname, p.parttype::text, p.partstrategy::text
FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_partition AS p ON p.parentid = c.oid
WHERE c.relkind = 'r' AND p.parttype IN ('r', 'p', 's', 'x')
  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
  AND (pg_catalog.pg_has_role(c.relowner, 'USAGE')
       OR pg_catalog.has_table_privilege(c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
       OR pg_catalog.has_any_column_privilege(c.oid, 'SELECT, INSERT, UPDATE, REFERENCES'))
ORDER BY n.nspname, c.relname, p.parttype, p.relname`
	openGaussColumnsSQL = `
SELECT n.nspname, t.relname, a.attnum, a.attname,
       pg_catalog.format_type(a.atttypid, a.atttypmod),
       CASE
         WHEN ty.typtype = 'd' THEN
           CASE WHEN bty.typelem <> 0 AND bty.typlen = -1 THEN 'ARRAY'
                WHEN bn.nspname = 'pg_catalog' THEN pg_catalog.format_type(ty.typbasetype, NULL)
                ELSE 'USER-DEFINED' END
         ELSE
           CASE WHEN ty.typelem <> 0 AND ty.typlen = -1 THEN 'ARRAY'
                WHEN tn.nspname = 'pg_catalog' THEN pg_catalog.format_type(a.atttypid, NULL)
                ELSE 'USER-DEFINED' END
       END,
       NOT (a.attnotnull OR (ty.typtype = 'd' AND ty.typnotnull)),
       CASE WHEN ad.adgencol <> 's' THEN pg_catalog.pg_get_expr(ad.adbin, ad.adrelid) END,
       EXISTS (
         SELECT 1
         FROM pg_catalog.pg_depend AS d
         JOIN pg_catalog.pg_class AS seq ON seq.oid = d.objid AND seq.relkind = 'S'
         WHERE d.refobjid = t.oid AND d.refobjsubid = a.attnum AND d.deptype = 'a'
       ),
       CASE WHEN ad.adgencol = 's' THEN 'ALWAYS' ELSE 'NEVER' END,
       CASE WHEN ad.adgencol = 's' THEN pg_catalog.pg_get_expr(ad.adbin, ad.adrelid) END
FROM pg_catalog.pg_class AS t
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid
JOIN pg_catalog.pg_type AS ty ON ty.oid = a.atttypid
JOIN pg_catalog.pg_namespace AS tn ON tn.oid = ty.typnamespace
LEFT JOIN pg_catalog.pg_attrdef AS ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
LEFT JOIN pg_catalog.pg_type AS bty ON ty.typtype = 'd' AND bty.oid = ty.typbasetype
LEFT JOIN pg_catalog.pg_namespace AS bn ON bn.oid = bty.typnamespace
WHERE t.relkind = 'r' AND t.relpersistence = 'p' AND t.parttype = 'n'
  AND t.reldeltarelid = 0 AND t.relcudescrelid = 0
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
  AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY n.nspname, t.relname, a.attnum`
	openGaussKeysSQL = `
SELECT n.nspname, t.relname, c.conname, c.contype::text, a.attname, c.conkey::text
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
WHERE c.contype IN ('p', 'u')
  AND t.relkind = 'r' AND t.relpersistence = 'p' AND t.parttype = 'n'
  AND t.reldeltarelid = 0 AND t.relcudescrelid = 0
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
ORDER BY n.nspname, t.relname, c.conname, c.conkey::text, a.attnum`
	openGaussForeignKeysSQL = `
SELECT n.nspname, t.relname, c.conname,
       sa.attnum, sa.attname, rn.nspname, rt.relname, ra.attnum, ra.attname,
       CASE c.confupdtype WHEN 'a' THEN 'no action' WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade' WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default' END,
       CASE c.confdeltype WHEN 'a' THEN 'no action' WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade' WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default' END,
       c.conkey::text, c.confkey::text
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_class AS rt ON rt.oid = c.confrelid
JOIN pg_catalog.pg_namespace AS rn ON rn.oid = rt.relnamespace
JOIN pg_catalog.pg_attribute AS sa ON sa.attrelid = t.oid AND sa.attnum = ANY(c.conkey)
JOIN pg_catalog.pg_attribute AS ra ON ra.attrelid = rt.oid AND ra.attnum = ANY(c.confkey)
WHERE c.contype = 'f'
  AND t.relkind = 'r' AND t.relpersistence = 'p' AND t.parttype = 'n'
  AND t.reldeltarelid = 0 AND t.relcudescrelid = 0
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
ORDER BY n.nspname, t.relname, c.conname, c.conkey::text, sa.attnum`
	openGaussChecksSQL = `
SELECT n.nspname, t.relname, c.conname, pg_catalog.pg_get_constraintdef(c.oid, true)
FROM pg_catalog.pg_constraint AS c
JOIN pg_catalog.pg_class AS t ON t.oid = c.conrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
WHERE c.contype = 'c'
  AND t.relkind = 'r' AND t.relpersistence = 'p' AND t.parttype = 'n'
  AND t.reldeltarelid = 0 AND t.relcudescrelid = 0
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
ORDER BY n.nspname, t.relname, c.conname`
	openGaussIndexesSQL = `
SELECT n.nspname, t.relname, i.relname, x.indisunique, x.indisprimary,
       am.amname, a.attname, x.indkey::text,
       pg_catalog.pg_get_indexdef(i.oid),
       pg_catalog.pg_get_expr(x.indpred, x.indrelid, true),
       x.indnkeyatts, x.indnatts, x.indoption::text, x.indisvisible,
       x.indisusable, x.indisvalid, x.indisready, x.indisexclusion, x.indimmediate
FROM pg_catalog.pg_index AS x
JOIN pg_catalog.pg_class AS t ON t.oid = x.indrelid
JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_class AS i ON i.oid = x.indexrelid
JOIN pg_catalog.pg_am AS am ON am.oid = i.relam
LEFT JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid AND a.attnum = ANY(x.indkey)
WHERE i.relkind = 'i'
  AND t.relkind = 'r' AND t.relpersistence = 'p' AND t.parttype = 'n'
  AND t.reldeltarelid = 0 AND t.relcudescrelid = 0
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%orientation=column%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=ustore%'
  AND LOWER(COALESCE(t.reloptions::text, '')) NOT LIKE '%storage_type=mot%'
ORDER BY n.nspname, t.relname, i.relname, x.indkey::text, a.attnum`
)
