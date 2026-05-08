package webdav

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"openlist-tvbox/internal/config"
)

func TestClientListParsesPropfind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Depth") != "1" {
			t.Fatalf("depth = %q", r.Header.Get("Depth"))
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/Movies/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>Movies</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Movies/Series/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>Series</d:displayname><d:resourcetype><d:collection/></d:resourcetype><d:getlastmodified>Fri, 01 May 2026 10:00:00 GMT</d:getlastmodified></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Movies/a%20%23.mkv</d:href><d:propstat><d:status>HTTP/1.1 404 Not Found</d:status><d:prop><d:getetag/></d:prop></d:propstat><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>a #.mkv</d:displayname><d:resourcetype/><d:getcontentlength>12</d:getcontentlength></d:prop></d:propstat></d:response>
</d:multistatus>`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	items, err := client.List(context.Background(), config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL + "/dav"}, "/Movies", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d: %#v", len(items), items)
	}
	if items[0].Name != "Series" || items[0].Type != 1 {
		t.Fatalf("folder item = %#v", items[0])
	}
	if items[1].Name != "a #.mkv" || items[1].Type != 0 || items[1].Size != 12 {
		t.Fatalf("file item = %#v", items[1])
	}
}

func TestClientListCancelsInFlightPropfind(t *testing.T) {
	server, requested := blockingDAVServer(t, "PROPFIND")

	client := NewClient(server.Client(), nil)
	assertWebDAVCancel(t, requested, func(ctx context.Context) error {
		_, err := client.List(ctx, config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL}, "/Movies", "")
		return err
	})
}

func TestClientGetCancelsInFlightPropfind(t *testing.T) {
	server, requested := blockingDAVServer(t, "PROPFIND")

	client := NewClient(server.Client(), nil)
	assertWebDAVCancel(t, requested, func(ctx context.Context) error {
		_, err := client.Get(ctx, config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL}, "/Movies/a.mkv", "")
		return err
	})
}

func TestClientOpenCancelsInFlightReadStream(t *testing.T) {
	server, requested := blockingDAVServer(t, http.MethodGet)

	client := NewClient(server.Client(), nil)
	assertWebDAVCancel(t, requested, func(ctx context.Context) error {
		stream, err := client.Open(ctx, config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL}, "/Movies/a.mkv", http.MethodGet, "")
		if stream != nil {
			_ = stream.Body.Close()
		}
		return err
	})
}

func TestFileURLDoesNotDoubleEscapeServerPath(t *testing.T) {
	got, err := fileURL("https://dav.example.com/remote.php/dav/files/demo%20user", "/电影/a #.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "%2520") || strings.Contains(got, "%2523") {
		t.Fatalf("double escaped URL: %s", got)
	}
	want := "https://dav.example.com/remote.php/dav/files/demo%20user/%E7%94%B5%E5%BD%B1/a%20%23.mkv"
	if got != want {
		t.Fatalf("url = %s, want %s", got, want)
	}
}

func blockingDAVServer(t *testing.T, method string) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	requested := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			t.Errorf("method = %s, want %s", r.Method, method)
		}
		once.Do(func() { close(requested) })
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server, requested
}

func assertWebDAVCancel(t *testing.T, requested <-chan struct{}, call func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- call(ctx)
	}()
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for webdav request")
	}
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled webdav call")
	}
}

func TestGowebdavRootKeepsEscapedReservedCharacters(t *testing.T) {
	got, err := gowebdavRoot("https://dav.example.com/remote.php/dav/files/demo%23user/%3Froot")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://dav.example.com/remote.php/dav/files/demo%23user/%3Froot"
	if got != want {
		t.Fatalf("root = %s, want %s", got, want)
	}
}

func TestClientOpenForwardsRangeAndBasicAuth(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !ok || user != "demo" || pass != "secret" {
			t.Fatalf("basic auth = %t %q %q", ok, user, pass)
		}
		if r.Header.Get("Range") != "bytes=1-3" {
			t.Fatalf("range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 1-3/5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("bcd"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	stream, err := client.Open(context.Background(), config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL, AuthType: "password", User: "demo", Password: "secret"}, "/movie.mkv", http.MethodGet, "bytes=1-3")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", stream.StatusCode)
	}
	if stream.Header.Get("Content-Range") != "bytes 1-3/5" {
		t.Fatalf("content-range = %q", stream.Header.Get("Content-Range"))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want auth challenge and retry", requests)
	}
}

func TestClientOpenForwardsOpenEndedRange(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !ok || user != "demo" || pass != "secret" {
			t.Fatalf("basic auth = %t %q %q", ok, user, pass)
		}
		if r.Header.Get("Range") != "bytes=10-" {
			t.Fatalf("range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 10-99/100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("from-ten"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	stream, err := client.Open(context.Background(), config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL, AuthType: "password", User: "demo", Password: "secret"}, "/movie.mkv", http.MethodGet, "bytes=10-")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", stream.StatusCode)
	}
	if stream.Header.Get("Content-Range") != "bytes 10-99/100" {
		t.Fatalf("content-range = %q", stream.Header.Get("Content-Range"))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want auth challenge and retry", requests)
	}
}

func TestClientOpenHeadUsesDAVAuthFlow(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !ok || user != "demo" || pass != "secret" {
			t.Fatalf("basic auth = %t %q %q", ok, user, pass)
		}
		if r.Header.Get("Range") != "bytes=10-" {
			t.Fatalf("range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Length", "90")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	stream, err := client.Open(context.Background(), config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL, AuthType: "password", User: "demo", Password: "secret"}, "/movie.mkv", http.MethodHead, "bytes=10-")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", stream.StatusCode)
	}
	if stream.Header.Get("Content-Length") != "90" {
		t.Fatalf("content-length = %q", stream.Header.Get("Content-Length"))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want auth challenge and retry", requests)
	}
}

func TestClientOpenForwardsSuffixRange(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !ok || user != "demo" || pass != "secret" {
			t.Fatalf("basic auth = %t %q %q", ok, user, pass)
		}
		if r.Header.Get("Range") != "bytes=-65536" {
			t.Fatalf("range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 34464-99999/100000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("tail"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	stream, err := client.Open(context.Background(), config.Backend{ID: "dav", Type: config.BackendTypeWebDAV, Server: server.URL, AuthType: "password", User: "demo", Password: "secret"}, "/movie.mkv", http.MethodGet, "bytes=-65536")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", stream.StatusCode)
	}
	if stream.Header.Get("Content-Range") != "bytes 34464-99999/100000" {
		t.Fatalf("content-range = %q", stream.Header.Get("Content-Range"))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want auth challenge and retry", requests)
	}
}
