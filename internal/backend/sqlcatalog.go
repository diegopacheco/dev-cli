package backend

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type section struct {
	title string
	query string
}

type sqlCatalog struct {
	quote     string
	describe  string
	commands  [][2]string
	sections  []section
	functions []entry
}

var plainIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_.]*$`)

func isAll(input string) bool {
	return strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(input), ";")), "all")
}

func quoteName(name, quote string) string {
	if plainIdentifier.MatchString(name) {
		return name
	}
	return quote + name + quote
}

func (s *SQL) all(ctx context.Context) []Result {
	c := s.catalog
	ready := Result{Title: "ready queries", Columns: []string{"query", "what it shows"}, Wide: true}
	var parts []Result
	for _, sec := range c.sections {
		r, err := s.query(ctx, sec.query)
		if err != nil {
			r = Result{Message: "unavailable: " + err.Error()}
		}
		r.Title = sec.title
		parts = append(parts, r)
		if sec.title != "tables" {
			continue
		}
		for i, row := range r.Rows {
			if i >= 8 {
				break
			}
			name := quoteName(str(row[0]), c.quote)
			ready.Rows = append(ready.Rows,
				[]any{"SELECT * FROM " + name + " LIMIT 10;", "first 10 rows of " + str(row[0])},
				[]any{fmt.Sprintf(c.describe, name), "columns of " + str(row[0])},
			)
		}
	}
	ready.Message = "⌘K then type ready to load one into the editor"
	out := []Result{commandResult("commands", c.commands), ready}
	out = append(out, parts...)
	return append(out, entryResult("functions", c.functions))
}

var sqlCommonFunctions = []entry{
	{"count", "SELECT count(*) FROM t", "number of rows"},
	{"sum", "SELECT sum(total) FROM t", "sum of a column"},
	{"avg", "SELECT avg(total) FROM t", "average of a column"},
	{"min", "SELECT min(total) FROM t", "smallest value"},
	{"max", "SELECT max(total) FROM t", "largest value"},
	{"coalesce", "coalesce(nickname, name)", "first value that is not NULL"},
	{"nullif", "nullif(total, 0)", "NULL when both values are equal"},
	{"case", "CASE WHEN total > 100 THEN 'big' ELSE 'small' END", "conditional value"},
	{"cast", "CAST(total AS text)", "convert a value to another type"},
	{"lower", "lower(email)", "lowercase text"},
	{"upper", "upper(name)", "uppercase text"},
	{"length", "length(name)", "length of text"},
	{"trim", "trim(name)", "remove surrounding spaces"},
	{"substr", "substr(name, 1, 3)", "part of a text"},
	{"replace", "replace(path, '/', '.')", "replace every occurrence in a text"},
	{"round", "round(total, 2)", "round to a number of decimals"},
	{"abs", "abs(delta)", "absolute value"},
	{"row_number", "row_number() OVER (ORDER BY total DESC)", "position of each row in a window"},
	{"rank", "rank() OVER (PARTITION BY user_id ORDER BY total DESC)", "rank with gaps for ties"},
	{"dense_rank", "dense_rank() OVER (ORDER BY total DESC)", "rank without gaps"},
	{"lag", "lag(total) OVER (ORDER BY created_at)", "value of the previous row"},
	{"lead", "lead(total) OVER (ORDER BY created_at)", "value of the next row"},
}

func withCommon(extra ...entry) []entry {
	return append(append([]entry{}, sqlCommonFunctions...), extra...)
}

func pgSchemaFilter(column string) string {
	return fmt.Sprintf("%[1]s NOT IN ('pg_catalog', 'information_schema') AND %[1]s NOT LIKE 'pg_toast%%' AND %[1]s NOT LIKE 'pg_temp%%'", column)
}

var postgresCatalog = sqlCatalog{
	quote:    `"`,
	describe: "SELECT attname AS column, format_type(atttypid, atttypmod) AS type, attnotnull AS not_null FROM pg_attribute WHERE attrelid = '%s'::regclass AND attnum > 0 AND NOT attisdropped ORDER BY attnum;",
	commands: [][2]string{
		{"all", "list commands, ready queries, databases, schemas, tables, columns, indexes, constraints, sequences, routines, triggers, extensions, roles, sessions and functions"},
		{"<sql>;", "run statements; Enter runs once the text ends with ; and Ctrl-R runs without it"},
		{"EXPLAIN ANALYZE <select>;", "execution plan with real timings"},
		{"SHOW ALL;", "every server setting"},
		{"SHOW work_mem;", "one server setting"},
		{"SELECT * FROM pg_stat_activity;", "sessions and the query each one runs"},
		{"SELECT * FROM pg_locks;", "locks held and waited for"},
		{"SELECT * FROM pg_stat_user_tables;", "scans, inserts, live and dead rows per table"},
		{"SELECT * FROM pg_stat_user_indexes;", "how often each index is used"},
		{"SELECT pg_size_pretty(pg_total_relation_size('<table>'));", "size of a table with its indexes"},
		{"SELECT pg_cancel_backend(<pid>);", "cancel the running query of a session"},
		{"SELECT pg_terminate_backend(<pid>);", "close a session"},
		{"VACUUM ANALYZE <table>;", "reclaim space and refresh planner statistics"},
		{"BEGIN; …; COMMIT;", "run statements in one transaction"},
	},
	sections: []section{
		{"server", `SELECT version() AS version, current_database() AS database, current_user AS "user", pg_size_pretty(pg_database_size(current_database())) AS size`},
		{"databases", "SELECT datname AS name, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database WHERE NOT datistemplate ORDER BY 1"},
		{"schemas", "SELECT schema_name AS name, schema_owner AS owner FROM information_schema.schemata WHERE " + pgSchemaFilter("schema_name") + " ORDER BY 1"},
		{"tables", `SELECT CASE WHEN n.nspname = 'public' THEN c.relname ELSE n.nspname || '.' || c.relname END AS name,
CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view' WHEN 'p' THEN 'partitioned table' ELSE 'foreign table' END AS type,
GREATEST(c.reltuples, 0)::bigint AS estimated_rows, pg_size_pretty(pg_total_relation_size(c.oid)) AS size
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'v', 'm', 'p', 'f') AND ` + pgSchemaFilter("n.nspname") + " ORDER BY 1"},
		{"columns", `SELECT table_name AS "table", column_name AS "column", data_type AS type, is_nullable AS nullable, column_default AS "default" FROM information_schema.columns WHERE ` + pgSchemaFilter("table_schema") + " ORDER BY table_schema, table_name, ordinal_position"},
		{"indexes", `SELECT tablename AS "table", indexname AS name, indexdef AS definition FROM pg_indexes WHERE ` + pgSchemaFilter("schemaname") + " ORDER BY 1, 2"},
		{"constraints", `SELECT table_name AS "table", constraint_name AS name, constraint_type AS type FROM information_schema.table_constraints WHERE ` + pgSchemaFilter("table_schema") + " AND constraint_type <> 'CHECK' ORDER BY 1, 2"},
		{"sequences", "SELECT sequence_schema AS schema, sequence_name AS name, data_type AS type FROM information_schema.sequences ORDER BY 1, 2"},
		{"routines", "SELECT routine_schema AS schema, routine_name AS name, routine_type AS type, data_type AS returns FROM information_schema.routines WHERE " + pgSchemaFilter("routine_schema") + " ORDER BY 1, 2"},
		{"triggers", `SELECT event_object_table AS "table", trigger_name AS name, action_timing AS timing, event_manipulation AS event FROM information_schema.triggers ORDER BY 1, 2`},
		{"extensions", "SELECT extname AS name, extversion AS version FROM pg_extension ORDER BY 1"},
		{"roles", "SELECT rolname AS name, rolsuper AS superuser, rolcanlogin AS login FROM pg_roles WHERE rolname NOT LIKE 'pg\\_%' ORDER BY 1"},
		{"sessions", `SELECT pid, usename AS "user", state, left(query, 80) AS query FROM pg_stat_activity WHERE datname = current_database() ORDER BY pid`},
	},
	functions: withCommon(
		entry{"now", "now()", "current timestamp with time zone"},
		entry{"date_trunc", "date_trunc('hour', created_at)", "cut a timestamp to a unit"},
		entry{"extract", "extract(epoch FROM created_at)", "one field of a timestamp"},
		entry{"age", "age(now(), created_at)", "interval between two timestamps"},
		entry{"to_char", "to_char(created_at, 'YYYY-MM-DD HH24:MI')", "format a timestamp or number"},
		entry{"generate_series", "SELECT * FROM generate_series(1, 10)", "rows from a range of numbers or timestamps"},
		entry{"string_agg", "string_agg(name, ', ')", "join texts of a group"},
		entry{"array_agg", "array_agg(id)", "array of a group"},
		entry{"unnest", "SELECT unnest(ARRAY[1, 2, 3])", "one row per array element"},
		entry{"->", "profile->'tags'", "JSON field as JSON"},
		entry{"->>", "profile->>'level'", "JSON field as text"},
		entry{"@>", `profile @> '{"lang": "go"}'`, "JSON contains another JSON"},
		entry{"jsonb_build_object", "jsonb_build_object('id', id, 'name', name)", "build a JSON object"},
		entry{"jsonb_agg", "jsonb_agg(u)", "JSON array of a group"},
		entry{"jsonb_array_elements", "SELECT jsonb_array_elements(profile->'tags')", "one row per JSON array element"},
		entry{"jsonb_pretty", "jsonb_pretty(profile)", "indented JSON text"},
		entry{"regexp_replace", "regexp_replace(email, '@.*', '')", "replace a regex match"},
		entry{"split_part", "split_part(email, '@', 2)", "one part of a split text"},
		entry{"gen_random_uuid", "gen_random_uuid()", "random UUID"},
		entry{"current_setting", "current_setting('work_mem')", "value of a server setting"},
	),
}

var mysqlCatalog = sqlCatalog{
	quote:    "`",
	describe: "DESCRIBE %s;",
	commands: [][2]string{
		{"all", "list commands, ready queries, server, databases, tables, columns, indexes, constraints, routines, triggers, events, users, sessions and functions"},
		{"<sql>;", "run statements; Enter runs once the text ends with ; and Ctrl-R runs without it"},
		{"EXPLAIN ANALYZE <select>;", "execution plan with real timings"},
		{"SHOW DATABASES;", "every database"},
		{"SHOW TABLES;", "tables of the current database"},
		{"SHOW CREATE TABLE <table>;", "the DDL of a table"},
		{"DESCRIBE <table>;", "columns of a table"},
		{"SHOW INDEX FROM <table>;", "indexes of a table"},
		{"SHOW FULL PROCESSLIST;", "sessions and the query each one runs"},
		{"SHOW VARIABLES LIKE '%timeout%';", "server variables matching a pattern"},
		{"SHOW GLOBAL STATUS LIKE 'Threads%';", "server counters matching a pattern"},
		{"SHOW ENGINE INNODB STATUS;", "InnoDB locks, transactions and buffers"},
		{"SHOW GRANTS;", "privileges of the current user"},
		{"KILL <id>;", "close a session"},
	},
	sections: []section{
		{"server", "SELECT VERSION() AS version, DATABASE() AS `database`, CURRENT_USER() AS `user`, @@hostname AS host"},
		{"databases", "SELECT schema_name AS name, default_character_set_name AS charset FROM information_schema.schemata ORDER BY 1"},
		{"tables", "SELECT table_name AS name, LOWER(table_type) AS type, engine AS engine, table_rows AS estimated_rows, CONCAT(ROUND((data_length + index_length) / 1024), ' KB') AS size FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY 1"},
		{"columns", "SELECT table_name AS `table`, column_name AS `column`, column_type AS type, is_nullable AS nullable, column_key AS `key`, column_default AS `default`, extra AS extra FROM information_schema.columns WHERE table_schema = DATABASE() ORDER BY table_name, ordinal_position"},
		{"indexes", "SELECT table_name AS `table`, index_name AS name, GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns, IF(non_unique = 0, 'yes', 'no') AS `unique` FROM information_schema.statistics WHERE table_schema = DATABASE() GROUP BY table_name, index_name, non_unique ORDER BY 1, 2"},
		{"constraints", "SELECT table_name AS `table`, constraint_name AS name, constraint_type AS type FROM information_schema.table_constraints WHERE table_schema = DATABASE() ORDER BY 1, 2"},
		{"routines", "SELECT routine_name AS name, routine_type AS type, data_type AS returns FROM information_schema.routines WHERE routine_schema = DATABASE() ORDER BY 1"},
		{"triggers", "SELECT event_object_table AS `table`, trigger_name AS name, action_timing AS timing, event_manipulation AS event FROM information_schema.triggers WHERE trigger_schema = DATABASE() ORDER BY 1, 2"},
		{"events", "SELECT event_name AS name, status AS status, interval_value AS every, interval_field AS unit FROM information_schema.events WHERE event_schema = DATABASE() ORDER BY 1"},
		{"users", "SELECT user AS name, host AS host FROM mysql.user ORDER BY 1, 2"},
		{"sessions", "SELECT id, user, host, db, command, time, state, LEFT(info, 80) AS query FROM performance_schema.processlist ORDER BY id"},
	},
	functions: withCommon(
		entry{"now", "NOW()", "current date and time"},
		entry{"date_format", "DATE_FORMAT(created_at, '%Y-%m-%d %H:%i')", "format a date"},
		entry{"date_add", "DATE_ADD(NOW(), INTERVAL 1 DAY)", "add an interval to a date"},
		entry{"timestampdiff", "TIMESTAMPDIFF(MINUTE, created_at, NOW())", "difference between two dates in a unit"},
		entry{"concat", "CONCAT(name, ' <', email, '>')", "join texts"},
		entry{"concat_ws", "CONCAT_WS(',', a, b, c)", "join texts with a separator"},
		entry{"group_concat", "GROUP_CONCAT(name ORDER BY name SEPARATOR ', ')", "join texts of a group"},
		entry{"ifnull", "IFNULL(nickname, name)", "second value when the first is NULL"},
		entry{"if", "IF(total > 100, 'big', 'small')", "one of two values"},
		entry{"->", "profile->'$.tags'", "JSON path as JSON"},
		entry{"->>", "profile->>'$.level'", "JSON path as text"},
		entry{"json_extract", "JSON_EXTRACT(profile, '$.tags[0]')", "value at a JSON path"},
		entry{"json_object", "JSON_OBJECT('id', id, 'name', name)", "build a JSON object"},
		entry{"json_arrayagg", "JSON_ARRAYAGG(name)", "JSON array of a group"},
		entry{"json_contains", `JSON_CONTAINS(profile, '"go"', '$.tags')`, "JSON contains a value"},
		entry{"json_table", "SELECT * FROM JSON_TABLE(profile, '$.tags[*]' COLUMNS (tag TEXT PATH '$')) t", "rows from a JSON array"},
		entry{"regexp_replace", "REGEXP_REPLACE(email, '@.*', '')", "replace a regex match"},
		entry{"uuid", "UUID()", "random UUID"},
		entry{"last_insert_id", "LAST_INSERT_ID()", "id generated by the last insert"},
	),
}

var sqliteCatalog = sqlCatalog{
	quote:    `"`,
	describe: "PRAGMA table_info(%s);",
	commands: [][2]string{
		{"all", "list commands, ready queries, server, databases, tables, columns, indexes, foreign keys, triggers, pragmas and functions"},
		{"<sql>;", "run statements; Enter runs once the text ends with ; and Ctrl-R runs without it"},
		{"EXPLAIN QUERY PLAN <select>;", "how SQLite will run a query"},
		{"PRAGMA table_info(<table>);", "columns of a table"},
		{"PRAGMA index_list(<table>);", "indexes of a table"},
		{"PRAGMA foreign_key_list(<table>);", "foreign keys of a table"},
		{"SELECT sql FROM sqlite_master WHERE name = '<table>';", "the DDL of a table"},
		{"PRAGMA integrity_check;", "check the database file for corruption"},
		{"PRAGMA journal_mode;", "rollback journal or WAL"},
		{"PRAGMA <name>;", "any pragma listed under pragmas"},
		{"ANALYZE;", "refresh planner statistics"},
		{"VACUUM;", "rebuild the file and reclaim space"},
		{"ATTACH DATABASE 'other.db' AS other;", "query a second database file"},
	},
	sections: []section{
		{"server", "SELECT sqlite_version() AS version, (SELECT file FROM pragma_database_list WHERE name = 'main') AS file, (SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()) AS bytes"},
		{"databases", "SELECT name, file FROM pragma_database_list"},
		{"tables", `SELECT m.name, m.type, (SELECT count(*) FROM pragma_table_info(m.name)) AS columns FROM sqlite_master m WHERE m.type IN ('table', 'view') AND m.name NOT LIKE 'sqlite_%' ORDER BY 1`},
		{"columns", `SELECT m.name AS "table", p.name AS "column", p.type AS type, CASE p."notnull" WHEN 1 THEN 'NO' ELSE 'YES' END AS nullable, p.pk AS pk, p.dflt_value AS "default" FROM sqlite_master m JOIN pragma_table_info(m.name) p WHERE m.type IN ('table', 'view') AND m.name NOT LIKE 'sqlite_%' ORDER BY m.name, p.cid`},
		{"indexes", `SELECT tbl_name AS "table", name, sql AS definition FROM sqlite_master WHERE type = 'index' ORDER BY 1, 2`},
		{"foreign keys", `SELECT m.name AS "table", f."from" AS "column", f."table" AS "references", f."to" AS "to" FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f WHERE m.type = 'table' ORDER BY 1`},
		{"triggers", `SELECT tbl_name AS "table", name, sql AS definition FROM sqlite_master WHERE type = 'trigger' ORDER BY 1, 2`},
		{"pragmas", "SELECT name FROM pragma_pragma_list ORDER BY 1"},
	},
	functions: withCommon(
		entry{"datetime", "datetime('now', '-1 hour')", "date and time text, with modifiers"},
		entry{"date", "date('now', 'start of month')", "date text"},
		entry{"strftime", "strftime('%Y-%m', created_at)", "format a date"},
		entry{"julianday", "julianday('now') - julianday(created_at)", "days as a number, handy for differences"},
		entry{"unixepoch", "unixepoch('now')", "seconds since 1970"},
		entry{"ifnull", "ifnull(nickname, name)", "second value when the first is NULL"},
		entry{"iif", "iif(total > 100, 'big', 'small')", "one of two values"},
		entry{"group_concat", "group_concat(name, ', ')", "join texts of a group"},
		entry{"printf", "printf('%.2f', total)", "format values into text"},
		entry{"instr", "instr(email, '@')", "position of a text inside another"},
		entry{"->", "meta->'cpu'", "JSON field as JSON"},
		entry{"->>", "meta->>'cpu'", "JSON field as a value"},
		entry{"json_extract", "json_extract(meta, '$.cpu')", "value at a JSON path"},
		entry{"json_each", "SELECT key, value FROM json_each(meta)", "one row per JSON member"},
		entry{"json_object", "json_object('id', id, 'name', name)", "build a JSON object"},
		entry{"json_group_array", "json_group_array(name)", "JSON array of a group"},
		entry{"typeof", "typeof(value)", "storage class of a value"},
		entry{"random", "abs(random()) % 100", "random integer"},
		entry{"last_insert_rowid", "last_insert_rowid()", "rowid of the last insert"},
		entry{"changes", "changes()", "rows changed by the last statement"},
	),
}
