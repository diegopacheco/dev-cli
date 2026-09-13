package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

type SQL struct {
	name     string
	driver   string
	target   string
	dsn      func(string) (string, error)
	wordsSQL string
	catalog  sqlCatalog
	db       *sql.DB
}

func NewMySQL(target string) *SQL {
	return &SQL{
		name:     "MySQL",
		driver:   "mysql",
		catalog:  mysqlCatalog,
		target:   target,
		dsn:      mysqlDSN,
		wordsSQL: "SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = DATABASE()",
	}
}

func NewPostgres(target string) *SQL {
	return &SQL{
		name:     "Postgres",
		driver:   "postgres",
		catalog:  postgresCatalog,
		target:   target,
		dsn:      func(s string) (string, error) { return s, nil },
		wordsSQL: "SELECT table_name, column_name FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog', 'information_schema')",
	}
}

func NewSQLite(target string) *SQL {
	return &SQL{
		name:     "SQLite",
		driver:   "sqlite3",
		catalog:  sqliteCatalog,
		target:   target,
		dsn:      func(s string) (string, error) { return s, nil },
		wordsSQL: "SELECT m.name, p.name FROM sqlite_master m JOIN pragma_table_info(m.name) p WHERE m.type IN ('table', 'view')",
	}
}

func mysqlDSN(target string) (string, error) {
	if !strings.HasPrefix(target, "mysql://") {
		return target, nil
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	pass, _ := u.User.Password()
	q := u.Query()
	q.Set("parseTime", "true")
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s", u.User.Username(), pass, u.Host, strings.TrimPrefix(u.Path, "/"), q.Encode()), nil
}

func (s *SQL) Name() string                  { return s.name }
func (s *SQL) Language() syntax.Language     { return syntax.SQL }
func (s *SQL) DefaultTarget() string         { return s.target }
func (s *SQL) RunsOnEnter(input string) bool { return EndsWithSemicolon(input) || isAll(input) }

func (s *SQL) Connect(ctx context.Context, target string) error {
	s.Close()
	dsn, err := s.dsn(target)
	if err != nil {
		return err
	}
	db, err := sql.Open(s.driver, dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return err
	}
	s.db = db
	s.target = target
	return nil
}

func (s *SQL) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func returnsRows(stmt string) bool {
	switch FirstWord(stmt) {
	case "SELECT", "SHOW", "WITH", "EXPLAIN", "DESCRIBE", "DESC", "PRAGMA", "VALUES", "TABLE":
		return true
	}
	return strings.Contains(strings.ToUpper(stmt), "RETURNING")
}

func (s *SQL) Execute(ctx context.Context, input string) ([]Result, error) {
	if s.db == nil {
		return nil, errors.New("not connected")
	}
	var results []Result
	for _, stmt := range SplitStatements(input) {
		if isAll(stmt) {
			results = append(results, s.all(ctx)...)
			continue
		}
		start := time.Now()
		var r Result
		var err error
		if returnsRows(stmt) {
			r, err = s.query(ctx, stmt)
		} else {
			var res sql.Result
			res, err = s.db.ExecContext(ctx, stmt)
			if err == nil {
				n, _ := res.RowsAffected()
				r.Message = fmt.Sprintf("%d rows affected", n)
			}
		}
		if err != nil {
			return results, fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
		r.Title = firstLine(stmt)
		r.Elapsed = time.Since(start)
		results = append(results, r)
	}
	return results, nil
}

func (s *SQL) query(ctx context.Context, stmt string) (Result, error) {
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return Result{}, err
	}
	types, _ := rows.ColumnTypes()
	r := Result{Columns: cols}
	for rows.Next() {
		if len(r.Rows) >= MaxRows {
			r.Message = fmt.Sprintf("showing first %d rows", MaxRows)
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return r, err
		}
		for i, v := range vals {
			if i < len(types) {
				vals[i] = typed(types[i].DatabaseTypeName(), v)
			}
		}
		r.Rows = append(r.Rows, vals)
	}
	if r.Message == "" {
		r.Message = fmt.Sprintf("%d rows", len(r.Rows))
	}
	return r, rows.Err()
}

func typed(dbType string, v any) any {
	b, ok := v.([]byte)
	if !ok {
		return v
	}
	switch strings.ToUpper(dbType) {
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT", "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE", "REAL", "INT2", "INT4", "INT8", "FLOAT4", "FLOAT8", "UNSIGNED INT", "UNSIGNED BIGINT":
		var n json.Number
		if json.Unmarshal(b, &n) == nil {
			return n
		}
	}
	return string(b)
}

func (s *SQL) Words(ctx context.Context) []string {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, s.wordsSQL)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var table, column string
		if rows.Scan(&table, &column) == nil {
			out = append(out, table, column)
		}
	}
	return Unique(out)
}

func firstLine(s string) string {
	line := strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(line) > 60 {
		line = line[:57] + "..."
	}
	return line
}
