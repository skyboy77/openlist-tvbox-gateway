package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/studio-b12/gowebdav"

	"openlist-tvbox/internal/config"
	"openlist-tvbox/internal/storage"
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

func (c *Client) List(ctx context.Context, backend config.Backend, p, _ string) ([]storage.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := c.newDAVClient(ctx, backend, nil)
	if err != nil {
		return nil, err
	}
	files, err := client.ReadDir(cleanDAVPath(p))
	if err != nil {
		c.logWarn("webdav propfind request failed", backend)
		return nil, sanitizeError(ctx, err)
	}
	parent := cleanDAVPath(p)
	items := make([]storage.Item, 0, len(files))
	for _, file := range files {
		items = append(items, fileInfoToItem(file, parent))
	}
	return items, nil
}

func (c *Client) RefreshList(ctx context.Context, backend config.Backend, p, password string) ([]storage.Item, error) {
	return c.List(ctx, backend, p, password)
}

func (c *Client) Get(ctx context.Context, backend config.Backend, p, _ string) (storage.Item, error) {
	if err := ctx.Err(); err != nil {
		return storage.Item{}, err
	}
	client, err := c.newDAVClient(ctx, backend, nil)
	if err != nil {
		return storage.Item{}, err
	}
	file, err := client.Stat(cleanDAVPath(p))
	if err != nil {
		c.logWarn("webdav propfind request failed", backend)
		return storage.Item{}, sanitizeError(ctx, err)
	}
	return fileInfoToItem(file, cleanDAVPath(path.Dir(cleanDAVPath(p)))), nil
}

func (c *Client) Search(context.Context, config.Backend, string, string, string) ([]storage.Item, error) {
	return nil, errors.New("webdav search is not supported")
}

func (c *Client) Open(ctx context.Context, backend config.Backend, p string, method string, rangeHeader string) (*Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, errors.New("unsupported webdav proxy method")
	}
	if method == http.MethodHead {
		return c.openHead(ctx, backend, p, rangeHeader)
	}
	if rangeHeader != "" {
		return c.openGetDirect(ctx, backend, p, rangeHeader)
	}

	capture := &responseCapture{}
	client, err := c.newDAVClient(ctx, backend, capture)
	if err != nil {
		return nil, err
	}

	davPath := cleanDAVPath(p)
	body, err := client.ReadStream(davPath)
	if err != nil {
		c.logWarn("webdav open request failed", backend)
		return nil, sanitizeError(ctx, err)
	}
	resp := capture.response()
	if resp == nil {
		body.Close()
		return nil, errors.New("webdav request failed")
	}
	return &Stream{Body: body, StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, nil
}

func (c *Client) openGetDirect(ctx context.Context, backend config.Backend, p string, rangeHeader string) (*Stream, error) {
	reqURL, err := fileURL(backend.Server, p)
	if err != nil {
		return nil, err
	}
	resp, err := c.doDAVRequest(ctx, backend, http.MethodGet, reqURL, cleanDAVPath(p), rangeHeader)
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

func (c *Client) doDAVRequest(ctx context.Context, backend config.Backend, method string, reqURL string, davPath string, rangeHeader string) (*http.Response, error) {
	auth, body := newDAVAuthorizer(backend).NewAuthenticator(nil)
	defer auth.Close()
	client := c.newDAVHTTPClient()
	for {
		req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		if err := auth.Authorize(client, req, davPath); err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		redo, err := auth.Verify(client, resp, davPath)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		if !redo {
			return resp, nil
		}
		resp.Body.Close()
	}
}

func (c *Client) openHead(ctx context.Context, backend config.Backend, p string, rangeHeader string) (*Stream, error) {
	reqURL, err := fileURL(backend.Server, p)
	if err != nil {
		return nil, err
	}
	resp, err := c.doDAVRequest(ctx, backend, http.MethodHead, reqURL, cleanDAVPath(p), rangeHeader)
	if err != nil {
		c.logWarn("webdav open request failed", backend)
		return nil, errors.New("webdav request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("webdav authorization failed; check backend credentials")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webdav returned status %d", resp.StatusCode)
	}
	return &Stream{Body: io.NopCloser(strings.NewReader("")), StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, nil
}

func (c *Client) newDAVClient(ctx context.Context, backend config.Backend, capture *responseCapture) (*gowebdav.Client, error) {
	root, err := gowebdavRoot(backend.Server)
	if err != nil {
		return nil, err
	}
	client := gowebdav.NewAuthClient(root, newDAVAuthorizer(backend))
	client.SetHeader("User-Agent", userAgent)
	client.SetTransport(captureTransport(ctx, c.http.Transport, capture))
	client.SetTimeout(c.http.Timeout)
	if c.http.Jar != nil {
		client.SetJar(c.http.Jar)
	}
	return client, nil
}

func newDAVAuthorizer(backend config.Backend) gowebdav.Authorizer {
	if backend.AuthType == "password" {
		return gowebdav.NewAutoAuth(backend.User, backend.Password)
	}
	return gowebdav.NewEmptyAuth()
}

func (c *Client) newDAVHTTPClient() *http.Client {
	return &http.Client{
		Transport: c.http.Transport,
		Timeout:   c.http.Timeout,
		Jar:       c.http.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return gowebdav.ErrTooManyRedirects
			}
			if len(via) > 0 && via[0].Header.Get(gowebdav.XInhibitRedirect) != "" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func captureTransport(ctx context.Context, base http.RoundTripper, capture *responseCapture) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if ctx != nil {
			req = req.WithContext(ctx)
		}
		resp, err := base.RoundTrip(req)
		if capture != nil && resp != nil {
			capture.set(resp)
		}
		return resp, err
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type responseCapture struct {
	resp *http.Response
}

func (c *responseCapture) set(resp *http.Response) {
	c.resp = resp
}

func (c *responseCapture) response() *http.Response {
	return c.resp
}

func fileInfoToItem(file os.FileInfo, parent string) storage.Item {
	itemType := 0
	if file.IsDir() {
		itemType = 1
	}
	modified := ""
	if !file.ModTime().IsZero() {
		modified = file.ModTime().Format(time.RFC3339)
	}
	return storage.Item{
		Name:     file.Name(),
		Parent:   parent,
		Path:     parent,
		Type:     itemType,
		Size:     file.Size(),
		Modified: modified,
	}
}

func sanitizeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}
	if gowebdav.IsErrCode(err, http.StatusUnauthorized) {
		return errors.New("webdav authorization failed; check backend credentials")
	}
	if gowebdav.IsErrCode(err, http.StatusForbidden) {
		return errors.New("webdav authorization failed; check backend credentials")
	}
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusPreconditionFailed,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if gowebdav.IsErrCode(err, code) {
			return fmt.Errorf("webdav returned status %d", code)
		}
	}
	return errors.New("webdav request failed")
}

func cleanDAVPath(p string) string {
	cleaned := path.Clean("/" + strings.Trim(p, "/"))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func gowebdavRoot(server string) (string, error) {
	u, err := parseServerURL(server)
	if err != nil {
		return "", err
	}
	escapedPath := u.EscapedPath()
	if escapedPath == "" {
		escapedPath = "/"
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(escapedPath, "/"), nil
}

func fileURL(server, p string) (string, error) {
	u, err := parseServerURL(server)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(u.Path, "/")
	rel := strings.Trim(cleanDAVPath(p), "/")
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

func parseServerURL(server string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("invalid webdav server URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.User != nil {
		return nil, errors.New("invalid webdav server URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("webdav server URL must not include query or fragment")
	}
	return u, nil
}

func (c *Client) logWarn(message string, backend config.Backend) {
	if c.logger != nil {
		c.logger.Warn(message, "backend", backend.ID, "auth_type", backend.AuthType)
	}
}
