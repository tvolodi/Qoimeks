package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

const (
	adminRateLimit   = 30
	adminMaxBody     = 8 << 10
	defaultPublicURL = "https://qoimeks.ai-dala.com"
)

// adminRoutes registers the tenant provisioning API for the ai-dala.com site. Without
// a ProvisioningToken the routes do not exist (404): the API is off, never open.
func (s *Server) adminRoutes(mux *http.ServeMux) {
	if s.ProvisioningToken == "" {
		return
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	limiter := &rateLimiter{n: adminRateLimit, window: time.Minute, now: now}
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			want := []byte("Bearer " + s.ProvisioningToken)
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
				writeAdminError(w, http.StatusUnauthorized, "unauthorized", "provisioning token is missing or wrong")
				return
			}
			if ok, wait := limiter.allow(); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
				writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "at most 30 requests per minute")
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("POST /admin/v1/tenants", guard(s.createTenant))
	mux.HandleFunc("POST /admin/v1/tenants/{id}/suspend", guard(s.setTenantStatus(store.TenantSuspended)))
	mux.HandleFunc("POST /admin/v1/tenants/{id}/resume", guard(s.setTenantStatus(store.TenantActive)))
	mux.HandleFunc("POST /admin/v1/tenants/{id}/rotate-code", guard(s.rotateTenantCode))
	mux.HandleFunc("GET /admin/v1/tenants/{id}", guard(s.getTenant))
}

func writeAdminError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

// adminStoreError maps store errors; internal ones are logged, never echoed.
func adminStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, "not_found", "unknown tenant")
	case errors.Is(err, store.ErrInvalid):
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		slog.Error("admin request failed", "path", r.URL.Path, "err", err)
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, adminMaxBody)
	var in struct {
		TenantID   string `json:"tenant_id"`
		Name       string `json:"name"`
		Language   string `json:"language"`
		Tier       string `json:"tier"`
		OwnerEmail string `json:"owner_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	t, code, created, err := s.Store.CreateTenant(r.Context(), store.TenantInput{
		ID: in.TenantID, Name: in.Name, Language: in.Language, Tier: in.Tier, OwnerEmail: in.OwnerEmail,
	})
	if err != nil {
		adminStoreError(w, r, err)
		return
	}
	serverURL := strings.TrimRight(s.PublicURL, "/")
	if serverURL == "" {
		serverURL = defaultPublicURL
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	body := map[string]string{
		"tenant_id":  t.ID.String(),
		"status":     t.Status,
		"server_url": serverURL,
	}
	if created {
		body["access_code"] = code // shown once: only its hash is stored
	}
	writeJSON(w, status, body)
}

func (s *Server) rotateTenantCode(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "not_found", "unknown tenant")
		return
	}
	code, err := s.Store.RotateTenantCode(r.Context(), id)
	if err != nil {
		adminStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"tenant_id": id.String(), "access_code": code})
}

func (s *Server) setTenantStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeAdminError(w, http.StatusNotFound, "not_found", "unknown tenant")
			return
		}
		if err := s.Store.SetTenantStatus(r.Context(), id, status); err != nil {
			adminStoreError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}

func (s *Server) getTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "not_found", "unknown tenant")
		return
	}
	t, err := s.Store.Tenant(r.Context(), id)
	if err != nil {
		adminStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"tenant_id":  t.ID.String(),
		"status":     t.Status,
		"tier":       t.Tier,
		"created_at": t.CreatedAt.UTC().Format(time.RFC3339),
	})
}
