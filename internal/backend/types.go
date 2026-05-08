package backend

import (
	"io"
	"net/http"
)

type OpenOptions struct {
	Method string
	Range  string
}

type Stream struct {
	Body       io.ReadCloser
	StatusCode int
	Header     http.Header
}
