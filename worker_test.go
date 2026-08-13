package main

import (
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

	want := "{\"create\":{\"_index\":\"myindex\"}}\n{\"message\":\"test\",\"level\":\"info\"}\n"
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

func TestBuildVictoriaLogsBodyDocContentEncoding(t *testing.T) {
	req := &RelayRequest{
		Method:  "POST",
		Url:     "/myindex/_doc/123",
		Headers: map[string][]string{"Content-Encoding": {"gzip"}},
		Body:    []byte("compressed"),
	}

	if _, err := buildVictoriaLogsBody(vlKindDoc, req); err == nil {
		t.Error("expected error for Content-Encoding on _doc request")
	}
}

func TestBuildVictoriaLogsBodyBulkPassthrough(t *testing.T) {
	raw := "{\"index\":{\"_index\":\"myindex\"}}\n{\"message\":\"bulk test\"}\n"
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

func TestSendToVictoriaLogs(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(200)
	}))
	defer server.Close()

	serverUrl, _ := url.Parse(server.URL)
	serverUrl.User = url.UserPassword("user", "pass")

	req := &RelayRequest{
		Method: "POST",
		Url:    "/myindex/_doc/123",
		Headers: map[string][]string{
			"Authorization": {"Basic ZWxhc3RpYzplcy1wYXNz"}, // original ES credentials, must not leak to VL
		},
		Body: []byte(`{"message":"test"}`),
	}

	client := &http.Client{Timeout: time.Second}
	if err := sendToVictoriaLogs(client, serverUrl.String(), vlKindDoc, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/insert/elasticsearch/_bulk" {
		t.Errorf("path = %q, want /insert/elasticsearch/_bulk", gotPath)
	}
	wantAuth := "Basic dXNlcjpwYXNz" // user:pass from victoriaLogsUrl
	if gotAuth != wantAuth {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantAuth)
	}
	wantBody := "{\"create\":{\"_index\":\"myindex\"}}\n{\"message\":\"test\"}\n"
	if gotBody != wantBody {
		t.Errorf("body = %q, want %q", gotBody, wantBody)
	}
}
