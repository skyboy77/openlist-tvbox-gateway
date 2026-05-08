package gateway

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"openlist-tvbox/internal/catvod"
	"openlist-tvbox/internal/mount"
)

const fileProxyTokenTTL = 12 * time.Hour

type fileProxyToken struct {
	Sub  string `json:"sub"`
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Exp  int64  `json:"exp"`
}

func (s *Server) fileProxyForSub(service *mount.Service, w http.ResponseWriter, r *http.Request, subID, proxyPath string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	tokenValue, _, ok := strings.Cut(proxyPath, "/")
	if !ok || tokenValue == "" {
		http.NotFound(w, r)
		return
	}
	token, ok := s.validFileProxyToken(tokenValue)
	if !ok || token.Sub != subID || token.ID == "" {
		writeJSON(w, http.StatusForbidden, catvod.Result{Error: "invalid proxy token"})
		return
	}
	rangeHeader := r.Header.Get("Range")
	if strings.Contains(rangeHeader, ",") {
		http.Error(w, "multiple ranges are not supported", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	stream, err := service.OpenProxyForSub(r.Context(), subID, token.ID, r.Method, rangeHeader)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("file proxy failed", "sub", subID, "error_kind", tvboxErrorKind(err))
		}
		writeJSON(w, http.StatusBadGateway, catvod.Result{Error: "file proxy failed"})
		return
	}
	defer stream.Body.Close()
	copyProxyHeaders(w.Header(), stream.Header)
	w.WriteHeader(stream.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, stream.Body)
	}
}

func (s *Server) issueFileProxyToken(subID, id, kind string) string {
	token := fileProxyToken{Sub: subID, ID: id, Kind: kind, Exp: time.Now().Add(fileProxyTokenTTL).Unix()}
	payload, _ := json.Marshal(token)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signAccessTokenPayload(encodedPayload)
	return base64.RawURLEncoding.EncodeToString([]byte(encodedPayload + "." + signature))
}

func (s *Server) validFileProxyToken(value string) (fileProxyToken, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return fileProxyToken{}, false
	}
	payload, signature, ok := strings.Cut(string(raw), ".")
	if !ok || signature == "" {
		return fileProxyToken{}, false
	}
	if !hmac.Equal([]byte(s.signAccessTokenPayload(payload)), []byte(signature)) {
		return fileProxyToken{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return fileProxyToken{}, false
	}
	var token fileProxyToken
	if json.Unmarshal(decoded, &token) != nil || token.Exp <= time.Now().Unix() {
		return fileProxyToken{}, false
	}
	if token.Kind != "media" && token.Kind != "subtitle" {
		return fileProxyToken{}, false
	}
	return token, true
}

func fileProxyName(name string) string {
	base := path.Base(strings.TrimSpace(name))
	if base == "." || base == "/" || base == "" {
		base = "file"
	}
	return base
}

func copyProxyHeaders(dst, src http.Header) {
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag", "Cache-Control"} {
		if value := src.Get(name); value != "" {
			if name == "Content-Length" {
				if _, err := strconv.ParseInt(value, 10, 64); err != nil {
					continue
				}
			}
			dst.Set(name, value)
		}
	}
}
