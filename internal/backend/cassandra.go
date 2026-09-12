package backend

import (
	"context"
	"errors"
	"fmt"
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
func (c *Cassandra) RunsOnEnter(input string) bool { return EndsWithSemicolon(input) }

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
