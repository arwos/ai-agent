package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPTools(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		body := `{"result":{"tools":[{"name":"echo","description":"test"}]},"instructions":"Use echo for tests"}`
		if r.Method == http.MethodGet {
			body = `{}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	m := Manager{}
	c := Config{Type: "http", Endpoint: "http://mock"}
	if e := m.Health(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	tools, e := m.ListTools(context.Background(), c)
	if e != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("%#v %v", tools, e)
	}
	if _, e = m.CallTool(context.Background(), c, "echo", map[string]any{}); e != nil {
		t.Fatal(e)
	}
}
