package admin

import (
	"net/http"

	"openlist-tvbox/internal/buildinfo"
)

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Current())
}
