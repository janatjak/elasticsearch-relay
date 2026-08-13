package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestVictoriaLogsKind(t *testing.T) {
	tests := []struct {
		method string
		url    string
		want   string
	}{
		{"POST", "/myindex/_doc/123", vlKindDoc},
		{"POST", "/myindex/_doc/123?refresh=true", vlKindDoc},
		{"POST", "/myindex/_doc/123#hash", vlKindDoc},
		{"POST", "/_bulk", vlKindBulk},
		{"POST", "/_bulk?refresh=true", vlKindBulk},
		{"POST", "/myindex/_bulk", vlKindBulk},
		{"PUT", "/myindex/_doc/123", vlKindNone},
		{"PUT", "/_bulk", vlKindNone},
		{"POST", "/myindex/_doc", vlKindNone},
		{"POST", "/myindex/_search", vlKindNone},
		{"POST", "/index/_doc/id/extra", vlKindNone},
		{"POST", "/", vlKindNone},
	}

	for _, tt := range tests {
		req := &RelayRequest{
			Method: tt.method,
			Url:    tt.url,
		}
		if got := victoriaLogsKind(req); got != tt.want {
			t.Errorf("victoriaLogsKind(%s, %s) = %q, want %q", tt.method, tt.url, got, tt.want)
		}
	}
}

func TestBuildVictoriaLogsBodyDoc(t *testing.T) {
	req := &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123?refresh=true",
		Body:   []byte("{\n  \"message\": \"test\",\n  \"level\": \"info\"\n}"),
	}

	body, err := buildVictoriaLogsBody(vlKindDoc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{\"create\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"test\",\"index\":\"myindex\",\"level\":\"info\",\"message\":\"test\"}\n"
	if string(body) != want {
		t.Errorf("buildVictoriaLogsBody = %q, want %q", body, want)
	}
}

func TestBuildVictoriaLogsBodyDocWithoutMessage(t *testing.T) {
	req := &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Body:   []byte(`{"level":"info"}`),
	}

	body, err := buildVictoriaLogsBody(vlKindDoc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{\"create\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"missing _msg\",\"index\":\"myindex\",\"level\":\"info\"}\n"
	if string(body) != want {
		t.Errorf("buildVictoriaLogsBody = %q, want %q", body, want)
	}
}

func TestBuildVictoriaLogsBodyBulkDefaultIndexFromPath(t *testing.T) {
	req := &RelayRequest{
		Method: "POST",
		Url:    "/pathindex/_bulk",
		Body:   []byte("{\"create\":{}}\n{\"message\":\"test\"}\n"),
	}

	body, err := buildVictoriaLogsBody(vlKindBulk, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{\"create\":{}}\n{\"_msg\":\"test\",\"index\":\"pathindex\",\"message\":\"test\"}\n"
	if string(body) != want {
		t.Errorf("buildVictoriaLogsBody = %q, want %q", body, want)
	}
}

func TestBuildVictoriaLogsBodyDocWithMsg(t *testing.T) {
	req := &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Body:   []byte(`{"message":"test","_msg":"already here"}`),
	}

	body, err := buildVictoriaLogsBody(vlKindDoc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{\"create\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"already here\",\"index\":\"myindex\",\"message\":\"test\"}\n"
	if string(body) != want {
		t.Errorf("buildVictoriaLogsBody = %q, want %q", body, want)
	}
}

func TestBuildVictoriaLogsBodyDocInvalidJson(t *testing.T) {
	req := &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Body:   []byte("not json"),
	}

	if _, err := buildVictoriaLogsBody(vlKindDoc, req); err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

func TestBuildVictoriaLogsBodyBulkPassthrough(t *testing.T) {
	// documents with _msg and no known index stay untouched
	raw := "{\"create\":{}}\n{\"_msg\":\"bulk test\"}\n"
	req := &RelayRequest{
		Method: "POST",
		Url:    "/_bulk",
		Body:   []byte(raw),
	}

	body, err := buildVictoriaLogsBody(vlKindBulk, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != raw {
		t.Errorf("bulk body changed: %q, want %q", body, raw)
	}
}

func TestBuildVictoriaLogsBodyBulkAddsMsg(t *testing.T) {
	raw := "{\"index\":{\"_index\":\"myindex\"}}\n{\"message\":\"bulk test\"}\n" +
		"{\"delete\":{\"_index\":\"myindex\",\"_id\":\"1\"}}\n" +
		"{\"create\":{}}\n{\"message\":\"second\"}"
	req := &RelayRequest{
		Method: "POST",
		Url:    "/_bulk",
		Body:   []byte(raw),
	}

	body, err := buildVictoriaLogsBody(vlKindBulk, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{\"index\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"bulk test\",\"index\":\"myindex\",\"message\":\"bulk test\"}\n" +
		"{\"delete\":{\"_index\":\"myindex\",\"_id\":\"1\"}}\n" +
		"{\"create\":{}}\n{\"_msg\":\"second\",\"message\":\"second\"}\n"
	if string(body) != want {
		t.Errorf("bulk body = %q, want %q", body, want)
	}
}

func gzipBytes(t *testing.T, data string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBuildVictoriaLogsBodyGzip(t *testing.T) {
	raw := "{\"create\":{}}\n{\"_msg\":\"bulk test\"}\n"
	req := &RelayRequest{
		Method:  "POST",
		Url:     "/_bulk",
		Headers: map[string][]string{"Content-Encoding": {"gzip"}},
		Body:    gzipBytes(t, raw),
	}

	body, err := buildVictoriaLogsBody(vlKindBulk, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != raw {
		t.Errorf("gzip bulk body = %q, want %q", body, raw)
	}
}

func TestBuildVictoriaLogsBodyUnsupportedEncoding(t *testing.T) {
	req := &RelayRequest{
		Method:  "POST",
		Url:     "/_bulk",
		Headers: map[string][]string{"Content-Encoding": {"br"}},
		Body:    []byte("compressed"),
	}

	if _, err := buildVictoriaLogsBody(vlKindBulk, req); err == nil {
		t.Error("expected error for unsupported Content-Encoding")
	}
}

type vlTestServer struct {
	server   *httptest.Server
	status   int
	requests []struct {
		path  string
		query string
		auth  string
		body  string
	}
}

func newVlTestServer() *vlTestServer {
	s := &vlTestServer{status: 200}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.requests = append(s.requests, struct {
			path  string
			query string
			auth  string
			body  string
		}{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(body)})
		w.WriteHeader(s.status)
	}))
	return s
}

func (s *vlTestServer) urlWithAuth() string {
	u, _ := url.Parse(s.server.URL)
	u.User = url.UserPassword("user", "pass")
	return u.String()
}

func newTestBatcher(s *vlTestServer) *vlBatcher {
	return newVlBatcher(&http.Client{Timeout: time.Second}, s.urlWithAuth(), false)
}

func TestVlBatcherFlush(t *testing.T) {
	s := newVlTestServer()
	defer s.server.Close()
	batcher := newTestBatcher(s)

	batcher.Add(vlKindDoc, &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Headers: map[string][]string{
			"Authorization": {"Basic ZWxhc3RpYzplcy1wYXNz"}, // original ES credentials, must not leak to VL
		},
		Body: []byte(`{"message":"single doc"}`),
	})
	// bulk body without a trailing newline -> batcher must add one
	batcher.Add(vlKindBulk, &RelayRequest{
		Method: "POST",
		Url:    "/_bulk",
		Body:   []byte("{\"index\":{\"_index\":\"myindex\"}}\n{\"message\":\"bulk test\"}"),
	})

	if len(s.requests) != 0 {
		t.Fatalf("expected no requests before flush, got %d", len(s.requests))
	}

	batcher.Flush()

	if len(s.requests) != 1 {
		t.Fatalf("expected 1 request after flush, got %d", len(s.requests))
	}
	req := s.requests[0]
	if req.path != "/insert/elasticsearch/_bulk" {
		t.Errorf("path = %q, want /insert/elasticsearch/_bulk", req.path)
	}
	if req.query != "_stream_fields=index" {
		t.Errorf("query = %q, want _stream_fields=index", req.query)
	}
	wantAuth := "Basic dXNlcjpwYXNz" // user:pass from victoriaLogsUrl
	if req.auth != wantAuth {
		t.Errorf("Authorization = %q, want %q", req.auth, wantAuth)
	}
	wantBody := "{\"create\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"single doc\",\"index\":\"myindex\",\"message\":\"single doc\"}\n" +
		"{\"index\":{\"_index\":\"myindex\"}}\n{\"_msg\":\"bulk test\",\"index\":\"myindex\",\"message\":\"bulk test\"}\n"
	if req.body != wantBody {
		t.Errorf("body = %q, want %q", req.body, wantBody)
	}
	if batcher.buf.Len() != 0 {
		t.Errorf("buffer not empty after successful flush: %d bytes", batcher.buf.Len())
	}
}

func TestVlBatcherFlushIfDue(t *testing.T) {
	s := newVlTestServer()
	defer s.server.Close()
	batcher := newTestBatcher(s)

	batcher.Add(vlKindDoc, &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Body:   []byte(`{"message":"test"}`),
	})

	batcher.FlushIfDue()
	if len(s.requests) != 0 {
		t.Fatalf("expected no flush before interval, got %d requests", len(s.requests))
	}

	batcher.lastFlush = time.Now().Add(-vlFlushInterval - time.Second)
	batcher.FlushIfDue()
	if len(s.requests) != 1 {
		t.Fatalf("expected flush after interval, got %d requests", len(s.requests))
	}
}

func TestVlBatcherFlushErrorKeepsBuffer(t *testing.T) {
	s := newVlTestServer()
	defer s.server.Close()
	batcher := newTestBatcher(s)

	batcher.Add(vlKindDoc, &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Body:   []byte(`{"message":"test"}`),
	})

	s.status = 500
	batcher.Flush()
	if batcher.buf.Len() == 0 {
		t.Fatal("buffer dropped after failed flush")
	}

	s.status = 200
	batcher.Flush()
	if batcher.buf.Len() != 0 {
		t.Fatal("buffer not empty after successful retry")
	}
	if len(s.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(s.requests))
	}
	if s.requests[0].body != s.requests[1].body {
		t.Errorf("retry body differs: %q vs %q", s.requests[0].body, s.requests[1].body)
	}
}
