package backend

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"openlist-tvbox/internal/config"
	"openlist-tvbox/internal/openlist"
	"openlist-tvbox/internal/webdav"
)

type Client struct {
	openlist *openlist.Client
	webdav   *webdav.Client
}

func NewClient(httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		openlist: openlist.NewClient(httpClient, logger),
		webdav:   webdav.NewClient(httpClient, logger),
	}
}

func (c *Client) List(ctx context.Context, backend config.Backend, path, password string) ([]openlist.Item, error) {
	if backend.Type == "webdav" {
		return c.webdav.List(ctx, backend, path, password)
	}
	return c.openlist.List(ctx, backend, path, password)
}

func (c *Client) RefreshList(ctx context.Context, backend config.Backend, path, password string) ([]openlist.Item, error) {
	if backend.Type == "webdav" {
		return nil, errors.New("webdav refresh is not supported")
	}
	return c.openlist.RefreshList(ctx, backend, path, password)
}

func (c *Client) Get(ctx context.Context, backend config.Backend, path, password string) (openlist.Item, error) {
	if backend.Type == "webdav" {
		return c.webdav.Get(ctx, backend, path, password)
	}
	return c.openlist.Get(ctx, backend, path, password)
}

func (c *Client) Search(ctx context.Context, backend config.Backend, path, keyword, password string) ([]openlist.Item, error) {
	if backend.Type == "webdav" {
		return nil, errors.New("webdav search is not supported")
	}
	return c.openlist.Search(ctx, backend, path, keyword, password)
}

func (c *Client) Open(ctx context.Context, backend config.Backend, path string, opts OpenOptions) (*Stream, error) {
	if backend.Type != "webdav" {
		return nil, errors.New("file proxy is not supported for this backend")
	}
	stream, err := c.webdav.Open(ctx, backend, path, opts.Method, opts.Range)
	if err != nil {
		return nil, err
	}
	return &Stream{Body: stream.Body, StatusCode: stream.StatusCode, Header: stream.Header}, nil
}
