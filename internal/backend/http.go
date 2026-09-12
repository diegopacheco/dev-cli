package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type httpAPI struct {
	base   *url.URL
	token  string
	client *http.Client
}

func newHTTPAPI(target, token string) (*httpAPI, error) {
	u, err := url.Parse(strings.TrimRight(target, "/"))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("target must look like http://host:port, got %q", target)
	}
	return &httpAPI{base: u, token: token, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (h *httpAPI) do(ctx context.Context, method, path string, query url.Values, body []byte) (any, error) {
	u := *h.base
	u.User = nil
	p, rawQuery, _ := strings.Cut(path, "?")
	u.Path = strings.TrimRight(h.base.Path, "/") + p
	q, _ := url.ParseQuery(rawQuery)
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	} else if h.base.User != nil {
		pass, _ := h.base.User.Password()
		req.SetBasicAuth(h.base.User.Username(), pass)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", method, p, resp.StatusCode, msg)
	}
	v, err := syntax.Decode(data)
	if err != nil {
		return strings.TrimSpace(string(data)), nil
	}
	return v, nil
}

func field(v any, path ...string) any {
	for _, key := range path {
		obj, ok := v.(syntax.Object)
		if !ok {
			return nil
		}
		v = nil
		for _, p := range obj {
			if p.Key == key {
				v = p.Value
				break
			}
		}
	}
	return v
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return syntax.Scalar(v)
}

func list(v any) []any {
	arr, _ := v.([]any)
	return arr
}
