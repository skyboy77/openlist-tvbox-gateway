package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
  <d:response><d:href>/dav/Movies/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Movies/Series/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><d:getlastmodified>Fri, 01 May 2026 10:00:00 GMT</d:getlastmodified></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Movies/a%20%23.mkv</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>12</d:getcontentlength></d:prop></d:propstat></d:response>
</d:multistatus>`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	items, err := client.List(context.Background(), config.Backend{ID: "dav", Type: "webdav", Server: server.URL + "/dav"}, "/Movies", "")
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

func TestClientOpenForwardsRangeAndBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
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
	stream, err := client.Open(context.Background(), config.Backend{ID: "dav", Type: "webdav", Server: server.URL, AuthType: "password", User: "demo", Password: "secret"}, "/movie.mkv", http.MethodGet, "bytes=1-3")
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
}
