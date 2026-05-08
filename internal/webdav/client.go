package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"openlist-tvbox/internal/config"
	"openlist-tvbox/internal/openlist"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124 Safari/537.36"

type Client struct {
	http   *http.Client
	logger *slog.Logger
}

type Stream struct {
	Body       io.ReadCloser
	StatusCode int
	Header     http.Header
}

func NewClient(httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{http: httpClient, logger: logger}
}

func (c *Client) List(ctx context.Context, backend config.Backend, p, _ string) ([]openlist.Item, error) {
	return c.propfind(ctx, backend, p, "1")
}

func (c *Client) RefreshList(ctx context.Context, backend config.Backend, p, password string) ([]openlist.Item, error) {
	return c.List(ctx, backend, p, password)
}

func (c *Client) Get(ctx context.Context, backend config.Backend, p, _ string) (openlist.Item, error) {
	items, err := c.propfind(ctx, backend, p, "0")
	if err != nil {
		return openlist.Item{}, err
	}
	if len(items) == 0 {
		return openlist.Item{Name: path.Base(strings.TrimRight(p, "/")), Path: path.Dir(p)}, nil
	}
	return items[0], nil
}

func (c *Client) Search(context.Context, config.Backend, string, string, string) ([]openlist.Item, error) {
	return nil, errors.New("webdav search is not supported")
}

func (c *Client) Open(ctx context.Context, backend config.Backend, p string, method string, rangeHeader string) (*Stream, error) {
	if method != http.MethodGet && method != http.MethodHead {
		return nil, errors.New("unsupported webdav proxy method")
	}
	reqURL, err := fileURL(backend.Server, p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	authorize(req, backend)
	resp, err := c.http.Do(req)
	if err != nil {
		c.logWarn("webdav open request failed", backend)
		return nil, errors.New("webdav request failed")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errors.New("webdav authorization failed; check backend credentials")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("webdav returned status %d", resp.StatusCode)
	}
	return &Stream{Body: resp.Body, StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, nil
}

func (c *Client) propfind(ctx context.Context, backend config.Backend, p, depth string) ([]openlist.Item, error) {
	reqURL, err := fileURL(backend.Server, p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", reqURL, strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
	req.Header.Set("User-Agent", userAgent)
	authorize(req, backend)
	resp, err := c.http.Do(req)
	if err != nil {
		c.logWarn("webdav propfind request failed", backend)
		return nil, errors.New("webdav request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("webdav authorization failed; check backend credentials")
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("webdav returned status %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, errors.New("invalid webdav response")
	}
	items, err := parseMultiStatus(payload, backend.Server, p, depth)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func authorize(req *http.Request, backend config.Backend) {
	if backend.AuthType == "password" {
		req.SetBasicAuth(backend.User, backend.Password)
	}
}

func (c *Client) logWarn(message string, backend config.Backend) {
	if c.logger != nil {
		c.logger.Warn(message, "backend", backend.ID, "auth_type", backend.AuthType)
	}
}

func fileURL(server, p string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid webdav server URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.User != nil {
		return "", errors.New("invalid webdav server URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("webdav server URL must not include query or fragment")
	}
	base := strings.TrimRight(u.Path, "/")
	rel := strings.Trim(path.Clean("/"+strings.Trim(p, "/")), "/")
	if rel != "" {
		for _, segment := range strings.Split(rel, "/") {
			base += "/" + segment
		}
	}
	if base == "" {
		base = "/"
	}
	u.Path = base
	u.RawPath = ""
	return u.String(), nil
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:getcontenttype/></d:prop></d:propfind>`

type multistatus struct {
	Responses []response `xml:"response"`
}

type response struct {
	Href     string     `xml:"href"`
	Propstat []propstat `xml:"propstat"`
}

type propstat struct {
	Prop prop `xml:"prop"`
}

type prop struct {
	ResourceType  resourceType `xml:"resourcetype"`
	ContentLength int64        `xml:"getcontentlength"`
	LastModified  string       `xml:"getlastmodified"`
}

type resourceType struct {
	Collection *struct{} `xml:"collection"`
}

func parseMultiStatus(payload []byte, server, requestPath, depth string) ([]openlist.Item, error) {
	var ms multistatus
	if err := xml.Unmarshal(payload, &ms); err != nil {
		return nil, errors.New("invalid webdav response")
	}
	requestPath = path.Clean("/" + strings.Trim(requestPath, "/"))
	out := make([]openlist.Item, 0, len(ms.Responses))
	for _, resp := range ms.Responses {
		name, parent, skip := itemPathFromHref(server, resp.Href, requestPath, depth)
		if skip {
			continue
		}
		pr := firstProp(resp)
		itemType := 0
		if pr.ResourceType.Collection != nil {
			itemType = 1
		}
		out = append(out, openlist.Item{
			Name:     name,
			Parent:   parent,
			Path:     parent,
			Type:     itemType,
			Size:     pr.ContentLength,
			Modified: parseHTTPTime(pr.LastModified),
		})
	}
	return out, nil
}

func firstProp(resp response) prop {
	if len(resp.Propstat) == 0 {
		return prop{}
	}
	return resp.Propstat[0].Prop
}

func itemPathFromHref(server, href, requestPath, depth string) (string, string, bool) {
	u, err := url.Parse(href)
	if err != nil {
		return "", "", true
	}
	serverURL, _ := url.Parse(server)
	hrefPath := u.EscapedPath()
	if hrefPath == "" && u.Opaque != "" {
		hrefPath = u.Opaque
	}
	decoded, err := url.PathUnescape(hrefPath)
	if err != nil {
		return "", "", true
	}
	base := ""
	if serverURL != nil {
		base, _ = url.PathUnescape(serverURL.EscapedPath())
	}
	if base != "" && strings.HasPrefix(decoded, strings.TrimRight(base, "/")+"/") {
		decoded = strings.TrimPrefix(decoded, strings.TrimRight(base, "/"))
	}
	decoded = path.Clean("/" + strings.Trim(decoded, "/"))
	if depth == "1" && decoded == requestPath {
		return "", "", true
	}
	name := path.Base(decoded)
	if name == "." || name == "/" || name == "" {
		return "", "", true
	}
	parent := path.Dir(decoded)
	if parent == "." {
		parent = "/"
	}
	return name, parent, false
}

func parseHTTPTime(raw string) string {
	if raw == "" {
		return ""
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t.Format(time.RFC3339)
	}
	return ""
}
