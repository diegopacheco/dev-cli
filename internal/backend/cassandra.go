package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
	"github.com/gocql/gocql"
)

type Cassandra struct {
	target   string
	keyspace string
	hosts    []string
	session  *gocql.Session
}

func NewCassandra(target string) *Cassandra {
	return &Cassandra{target: target}
}

func (c *Cassandra) Name() string                  { return "Cassandra" }
func (c *Cassandra) Language() syntax.Language     { return syntax.CQL }
func (c *Cassandra) DefaultTarget() string         { return c.target }
func (c *Cassandra) RunsOnEnter(input string) bool { return EndsWithSemicolon(input) || isAll(input) }

func ParseCassandraTarget(target string) ([]string, string) {
	hostPart, keyspace, _ := strings.Cut(strings.TrimPrefix(target, "cassandra://"), "/")
	return strings.Split(hostPart, ","), keyspace
}

func (c *Cassandra) Connect(ctx context.Context, target string) error {
	hosts, keyspace := ParseCassandraTarget(target)
	if err := c.open(hosts, keyspace); err != nil {
		return err
	}
	c.target = target
	return nil
}

func (c *Cassandra) open(hosts []string, keyspace string) error {
	c.Close()
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.One
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 5 * time.Second
	cluster.DisableInitialHostLookup = true
	cluster.ProtoVersion = 4
	session, err := cluster.CreateSession()
	if err != nil {
		return err
	}
	c.session, c.hosts, c.keyspace = session, hosts, keyspace
	return nil
}

func (c *Cassandra) Close() error {
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	return nil
}

func (c *Cassandra) Execute(ctx context.Context, input string) ([]Result, error) {
	if c.session == nil {
		return nil, errors.New("not connected")
	}
	var results []Result
	for _, stmt := range SplitStatements(input) {
		if isAll(stmt) {
			results = append(results, c.all(ctx)...)
			continue
		}
		start := time.Now()
		if FirstWord(stmt) == "USE" {
			ks := strings.Trim(strings.Fields(stmt)[len(strings.Fields(stmt))-1], `"`)
			if err := c.open(c.hosts, ks); err != nil {
				return results, err
			}
			results = append(results, Result{Title: stmt, Message: "keyspace " + ks, Elapsed: time.Since(start)})
			continue
		}
		r, err := c.query(ctx, stmt)
		if err != nil {
			return results, fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
		r.Title = firstLine(stmt)
		r.Elapsed = time.Since(start)
		results = append(results, r)
	}
	return results, nil
}

func (c *Cassandra) query(ctx context.Context, stmt string) (Result, error) {
	iter := c.session.Query(stmt).WithContext(ctx).PageSize(500).Iter()
	cols := iter.Columns()
	r := Result{}
	for _, col := range cols {
		r.Columns = append(r.Columns, col.Name)
	}
	for {
		if len(r.Rows) >= MaxRows {
			r.Message = fmt.Sprintf("showing first %d rows", MaxRows)
			break
		}
		m := map[string]any{}
		if !iter.MapScan(m) {
			break
		}
		row := make([]any, len(cols))
		for i, col := range cols {
			row[i] = m[col.Name]
		}
		r.Rows = append(r.Rows, row)
	}
	if err := iter.Close(); err != nil {
		return r, err
	}
	if len(cols) == 0 {
		r.Message = "OK"
	} else if r.Message == "" {
		r.Message = fmt.Sprintf("%d rows", len(r.Rows))
	}
	return r, nil
}

func (c *Cassandra) Words(ctx context.Context) []string {
	if c.session == nil {
		return nil
	}
	var out []string
	iter := c.session.Query("SELECT keyspace_name, table_name, column_name FROM system_schema.columns").WithContext(ctx).Iter()
	var ks, table, column string
	for iter.Scan(&ks, &table, &column) {
		if strings.HasPrefix(ks, "system") {
			continue
		}
		out = append(out, ks, table, column, ks+"."+table)
	}
	iter.Close()
	return Unique(out)
}

type cqlSection struct {
	title    string
	query    string
	userOnly bool
}

var cassandraCommands = [][2]string{
	{"all", "list commands, ready queries, cluster, peers, keyspaces, tables, columns, indexes, views, types, functions, aggregates and virtual tables"},
	{"<cql>;", "run statements; Enter runs once the text ends with ; and Ctrl-R runs without it"},
	{"USE <keyspace>;", "switch the keyspace"},
	{"DESCRIBE KEYSPACES;", "every keyspace"},
	{"DESCRIBE TABLES;", "every table"},
	{"DESCRIBE TABLE <keyspace.table>;", "the CREATE statement of a table"},
	{"DESCRIBE SCHEMA;", "the CREATE statements of every user keyspace"},
	{"SELECT JSON * FROM <table>;", "rows as JSON"},
	{"INSERT INTO <table> JSON '{…}';", "insert a row from JSON"},
	{"SELECT * FROM <table> WHERE <column> = … ALLOW FILTERING;", "filter on a column that is not part of the key"},
	{"SELECT token(<key>), writetime(<column>), ttl(<column>) FROM <table>;", "partition token, write time and time to live"},
	{"BEGIN BATCH …; APPLY BATCH;", "run writes together"},
	{"TRUNCATE <table>;", "delete every row of a table"},
	{"SELECT * FROM system_views.settings;", "every server setting"},
	{"SELECT * FROM system_views.clients;", "connected clients"},
}

var cassandraSections = []cqlSection{
	{"cluster", "SELECT cluster_name, release_version, data_center, rack, partitioner FROM system.local", false},
	{"peers", "SELECT peer, data_center, rack, release_version FROM system.peers", false},
	{"keyspaces", "SELECT keyspace_name, durable_writes, replication FROM system_schema.keyspaces", false},
	{"tables", "SELECT keyspace_name, table_name, default_time_to_live, gc_grace_seconds, comment FROM system_schema.tables", true},
	{"columns", "SELECT keyspace_name, table_name, column_name, kind, type FROM system_schema.columns", true},
	{"indexes", "SELECT keyspace_name, table_name, index_name, kind, options FROM system_schema.indexes", true},
	{"materialized views", "SELECT keyspace_name, view_name, base_table_name FROM system_schema.views", true},
	{"user types", "SELECT keyspace_name, type_name, field_names, field_types FROM system_schema.types", true},
	{"user functions", "SELECT keyspace_name, function_name, argument_types, return_type FROM system_schema.functions", true},
	{"aggregates", "SELECT keyspace_name, aggregate_name, argument_types, return_type FROM system_schema.aggregates", true},
	{"virtual tables", "SELECT keyspace_name, table_name, comment FROM system_virtual_schema.tables", false},
}

var cqlFunctions = []entry{
	{"count", "SELECT count(*) FROM users", "number of rows"},
	{"sum", "SELECT sum(total) FROM orders", "sum of a column"},
	{"avg", "SELECT avg(total) FROM orders", "average of a column"},
	{"min", "SELECT min(total) FROM orders", "smallest value"},
	{"max", "SELECT max(total) FROM orders", "largest value"},
	{"now", "now()", "new timeuuid for the current time"},
	{"uuid", "uuid()", "random UUID"},
	{"to_timestamp", "to_timestamp(now())", "timeuuid or date as a timestamp"},
	{"to_date", "to_date(now())", "timeuuid or timestamp as a date"},
	{"to_unix_timestamp", "to_unix_timestamp(now())", "milliseconds since 1970"},
	{"current_timestamp", "current_timestamp()", "current timestamp"},
	{"min_timeuuid", "min_timeuuid('2026-01-01')", "smallest timeuuid of a moment, for range scans"},
	{"max_timeuuid", "max_timeuuid('2026-01-01')", "largest timeuuid of a moment"},
	{"writetime", "writetime(name)", "microsecond time a column was written"},
	{"ttl", "ttl(name)", "seconds left before a column expires"},
	{"token", "token(id)", "partition token of a key"},
	{"to_json", "to_json(tags)", "a value as JSON text"},
	{"from_json", "from_json('[\"a\"]')", "a JSON text as a value, in INSERT or UPDATE"},
	{"text_as_blob", "text_as_blob(name)", "text as bytes"},
	{"blob_as_text", "blob_as_text(data)", "bytes as text"},
	{"map_keys", "map_keys(attributes)", "keys of a map"},
	{"map_values", "map_values(attributes)", "values of a map"},
	{"collection_count", "collection_count(tags)", "size of a list, set or map"},
	{"collection_min", "collection_min(scores)", "smallest element of a collection"},
	{"collection_max", "collection_max(scores)", "largest element of a collection"},
	{"abs", "abs(delta)", "absolute value"},
	{"round", "round(price)", "nearest whole number"},
	{"exp", "exp(rate)", "e raised to a power"},
	{"log", "log(value)", "natural logarithm"},
	{"mask_inner", "mask_inner(email, 1, 4)", "hide the middle of a text"},
	{"mask_outer", "mask_outer(email, 1, 4)", "hide the ends of a text"},
	{"mask_default", "mask_default(email)", "fixed placeholder for the column type"},
	{"mask_null", "mask_null(email)", "always NULL"},
	{"mask_hash", "mask_hash(email)", "hash of a value"},
	{"similarity_cosine", "similarity_cosine(embedding, [0.1, 0.2])", "cosine similarity of two vectors"},
}

func (c *Cassandra) all(ctx context.Context) []Result {
	ready := Result{Title: "ready queries", Columns: []string{"query", "what it shows"}, Wide: true}
	var parts []Result
	for _, sec := range cassandraSections {
		r, err := c.query(ctx, sec.query)
		if err != nil {
			r = Result{Message: "unavailable: " + err.Error()}
		}
		if sec.userOnly {
			r.Rows = slices.DeleteFunc(r.Rows, func(row []any) bool { return strings.HasPrefix(str(row[0]), "system") })
			r.Message = fmt.Sprintf("%d rows", len(r.Rows))
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
			name := str(row[0]) + "." + str(row[1])
			ready.Rows = append(ready.Rows,
				[]any{"SELECT * FROM " + name + " LIMIT 10;", "first 10 rows of " + name},
				[]any{"DESCRIBE TABLE " + name + ";", "CREATE statement of " + name},
			)
		}
	}
	ready.Rows = append(ready.Rows, []any{"SELECT * FROM system_views.clients;", "connected clients"})
	ready.Message = "⌘K then type ready to load one into the editor"
	out := []Result{commandResult("commands", cassandraCommands), ready}
	out = append(out, parts...)
	return append(out, entryResult("functions", cqlFunctions))
}
