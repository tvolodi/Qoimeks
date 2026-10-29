package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

func (s *Server) codesRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/codes", s.assignCode)
	mux.HandleFunc("GET /api/codes", s.listCodes)
	mux.HandleFunc("GET /api/codes/lookup", s.lookupCode)
	mux.HandleFunc("DELETE /api/codes/{id}", s.removeCode)
}

// writeReason is writeError plus a stable machine-readable reason, and any extra fields
// (the app localizes from reason and owner, not from the message).
func writeReason(w http.ResponseWriter, status int, msg, reason string, extra map[string]any) {
	body := map[string]any{"error": msg, "reason": reason}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// codeError maps a store error of the codes calls to a response with a reason.
func codeError(w http.ResponseWriter, r *http.Request, err error, notFoundReason string, extra map[string]any) {
	var inUse *store.CodeInUseError
	switch {
	case errors.As(err, &inUse):
		writeReason(w, http.StatusConflict, inUse.Error(), "code_in_use",
			map[string]any{"code": inUse.Code, "owner": inUse.Owner})
	case errors.Is(err, store.ErrNotFound):
		writeReason(w, http.StatusNotFound, "not found", notFoundReason, extra)
	case errors.Is(err, store.ErrConflict):
		writeReason(w, http.StatusConflict, err.Error(), "id_conflict", nil)
	case errors.Is(err, store.ErrInvalid):
		writeReason(w, http.StatusBadRequest, err.Error(), "invalid_code", nil)
	default:
		storeError(w, r, err)
	}
}

func (s *Server) assignCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID         uuid.UUID `json:"id"`
		ObjectType string    `json:"object_type"`
		ObjectID   int64     `json:"object_id"`
		CodeType   string    `json:"code_type"`
		Code       string    `json:"code"`
		Operator   string    `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeReason(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request", nil)
		return
	}
	c, created, err := s.store(r).AssignCode(r.Context(), store.NewCode{
		ID: in.ID, ObjectType: in.ObjectType, ObjectID: in.ObjectID, CodeType: in.CodeType,
		Code: strings.TrimSpace(in.Code), Operator: strings.TrimSpace(in.Operator),
	})
	if err != nil {
		codeError(w, r, err, "object_not_found", nil)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, c)
}

func (s *Server) listCodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := strconv.ParseInt(q.Get("object_id"), 10, 64)
	if err != nil {
		writeReason(w, http.StatusBadRequest, "object_id is required", "invalid_request", nil)
		return
	}
	codes, err := s.store(r).ListCodes(r.Context(), q.Get("object_type"), id)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeReason(w, http.StatusBadRequest, err.Error(), "invalid_request", nil)
			return
		}
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (s *Server) lookupCode(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeReason(w, http.StatusBadRequest, "code is required", "invalid_request", nil)
		return
	}
	c, err := s.store(r).LookupCode(r.Context(), code)
	if err != nil {
		codeError(w, r, err, "code_not_found", map[string]any{"code": code})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) removeCode(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeReason(w, http.StatusBadRequest, "invalid code id", "invalid_request", nil)
		return
	}
	operator := strings.TrimSpace(r.URL.Query().Get("operator"))
	if operator == "" {
		writeReason(w, http.StatusBadRequest, "operator is required", "invalid_request", nil)
		return
	}
	c, err := s.store(r).RemoveCode(r.Context(), id, operator)
	if err != nil {
		codeError(w, r, err, "code_not_found", nil)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
