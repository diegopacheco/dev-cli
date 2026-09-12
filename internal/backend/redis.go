package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type RedisError string

func (e RedisError) Error() string { return string(e) }

type RESP struct {
	conn net.Conn
	r    *bufio.Reader
}

func NewRESP(conn net.Conn) *RESP {
	return &RESP{conn: conn, r: bufio.NewReader(conn)}
}

func EncodeCommand(args []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return []byte(b.String())
}

func (c *RESP) Do(ctx context.Context, args ...string) (any, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(deadline)
	} else {
		c.conn.SetDeadline(time.Time{})
	}
	if _, err := c.conn.Write(EncodeCommand(args)); err != nil {
		return nil, err
	}
	return ReadReply(c.r)
}

func ReadReply(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(line, "\r\n")
	if line == "" {
		return nil, errors.New("empty reply")
	}
	body := line[1:]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return nil, RedisError(body)
	case ':':
		return json.Number(body), nil
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		arr := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := ReadReply(r)
			if err != nil {
				if _, isRedis := err.(RedisError); !isRedis {
					return nil, err
				}
				v = "(error) " + err.Error()
			}
			arr = append(arr, v)
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unknown reply type %q", line[0])
}

func SplitArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == '\\' && i+1 < len(runes) {
				i++
				switch runes[i] {
				case 'n':
					cur.WriteRune('\n')
				case 't':
					cur.WriteRune('\t')
				default:
					cur.WriteRune(runes[i])
				}
			} else if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

type Redis struct {
	mu     sync.Mutex
	target string
	client *RESP
}

func NewRedis(target string) *Redis {
	return &Redis{target: target}
}

func (r *Redis) Name() string                  { return "Redis" }
func (r *Redis) Language() syntax.Language     { return syntax.Redis }
func (r *Redis) DefaultTarget() string         { return r.target }
func (r *Redis) RunsOnEnter(input string) bool { return true }

func (r *Redis) Connect(ctx context.Context, target string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return err
	}
	client := NewRESP(conn)
	if pass, ok := u.User.Password(); ok {
		args := []string{"AUTH", pass}
		if name := u.User.Username(); name != "" {
			args = []string{"AUTH", name, pass}
		}
		if _, err := client.Do(ctx, args...); err != nil {
			conn.Close()
			return err
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" && db != "0" {
		if _, err := client.Do(ctx, "SELECT", db); err != nil {
			conn.Close()
			return err
		}
	}
	if _, err := client.Do(ctx, "PING"); err != nil {
		conn.Close()
		return err
	}
	r.client = client
	r.target = target
	return nil
}

func (r *Redis) closeLocked() {
	if r.client != nil {
		r.client.conn.Close()
		r.client = nil
	}
}

func (r *Redis) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
	return nil
}

func (r *Redis) do(ctx context.Context, args ...string) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil {
		return nil, errors.New("not connected")
	}
	v, err := r.client.Do(ctx, args...)
	if err != nil {
		if _, isRedis := err.(RedisError); !isRedis {
			r.closeLocked()
		}
	}
	return v, err
}

func (r *Redis) Execute(ctx context.Context, input string) ([]Result, error) {
	var results []Result
	for _, line := range Lines(input) {
		args, err := SplitArgs(line)
		if err != nil {
			return results, err
		}
		if len(args) == 0 {
			continue
		}
		start := time.Now()
		v, err := r.do(ctx, args...)
		if err != nil {
			return results, fmt.Errorf("%s: %w", line, err)
		}
		results = append(results, Result{Title: line, Value: shapeReply(strings.ToUpper(args[0]), v), Message: replySummary(v), Elapsed: time.Since(start)})
	}
	return results, nil
}

func shapeReply(cmd string, v any) any {
	arr, ok := v.([]any)
	if !ok || len(arr)%2 != 0 {
		return v
	}
	switch cmd {
	case "HGETALL", "CONFIG", "XINFO":
		obj := syntax.Object{}
		for i := 0; i < len(arr); i += 2 {
			obj = append(obj, syntax.Pair{Key: fmt.Sprint(arr[i]), Value: arr[i+1]})
		}
		return obj
	}
	return v
}

func replySummary(v any) string {
	switch x := v.(type) {
	case nil:
		return "(nil)"
	case []any:
		return fmt.Sprintf("%d items", len(x))
	case json.Number:
		return "(integer) " + string(x)
	}
	return "OK"
}

func (r *Redis) Words(ctx context.Context) []string {
	var keys []string
	cursor := "0"
	for i := 0; i < 20 && len(keys) < MaxRows; i++ {
		v, err := r.do(ctx, "SCAN", cursor, "COUNT", "200")
		if err != nil {
			return keys
		}
		arr, ok := v.([]any)
		if !ok || len(arr) != 2 {
			return keys
		}
		cursor = fmt.Sprint(arr[0])
		if batch, ok := arr[1].([]any); ok {
			for _, k := range batch {
				keys = append(keys, fmt.Sprint(k))
			}
		}
		if cursor == "0" {
			break
		}
	}
	return Unique(keys)
}
