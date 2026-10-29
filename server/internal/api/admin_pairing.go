package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

const (
	pairingMaxLabel   = 80
	pairingMinTTL     = 60
	pairingMaxTTL     = 900
	pairingRespLayout = time.RFC3339
)

// pairingRoutes registers the pairing and device admin calls under the provisioning guard.
func (s *Server) pairingRoutes(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /admin/v1/tenants/{id}/pairings", guard(s.createPairing))
	mux.HandleFunc("POST /admin/v1/tenants/{id}/pairings/{pairing_id}/cancel", guard(s.cancelPairing))
	mux.HandleFunc("GET /admin/v1/tenants/{id}/devices", guard(s.listDevices))
	mux.HandleFunc("POST /admin/v1/tenants/{id}/devices/{device_id}/revoke", guard(s.revokeDevice))
}

func (s *Server) publicURL() string {
	if u := strings.TrimRight(s.PublicURL, "/"); u != "" {
		return u
	}
	return defaultPublicURL
}

func notFound(w http.ResponseWriter, what string) {
	writeAdminError(w, http.StatusNotFound, "not_found", "unknown "+what)
}

// pairingTenant loads the tenant of the path. It answers 404 itself (bad id or unknown).
func (s *Server) pairingTenant(w http.ResponseWriter, r *http.Request) (store.Tenant, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w, "tenant")
		return store.Tenant{}, false
	}
	t, err := s.Store.Tenant(r.Context(), id)
	if err != nil {
		adminStoreError(w, r, err)
		return store.Tenant{}, false
	}
	return t, true
}

func pairingInternal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("admin request failed", "path", r.URL.Path, "err", err)
	writeAdminError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

func deviceJSON(d store.Device) map[string]any {
	ts := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.UTC().Format(pairingRespLayout)
	}
	return map[string]any{
		"device_id":    d.ID.String(),
		"label":        d.Label,
		"operator":     d.Operator,
		"app_version":  d.AppVersion,
		"created_at":   d.CreatedAt.UTC().Format(pairingRespLayout),
		"last_seen_at": ts(d.LastSeenAt),
		"revoked_at":   ts(d.RevokedAt),
	}
}

func (s *Server) createPairing(w http.ResponseWriter, r *http.Request) {
	t, ok := s.pairingTenant(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminMaxBody)
	var in struct {
		Label      string `json:"label"`
		TTLSeconds *int   `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len([]rune(in.Label)) > pairingMaxLabel {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "label is longer than 80 characters")
		return
	}
	ttl := store.DefaultPairingTTL
	if in.TTLSeconds != nil {
		if *in.TTLSeconds < pairingMinTTL || *in.TTLSeconds > pairingMaxTTL {
			writeAdminError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds must be between 60 and 900")
			return
		}
		ttl = time.Duration(*in.TTLSeconds) * time.Second
	}
	now := s.clock()
	if st := t.EffectiveStatus(now); st != store.TenantActive {
		writeAdminError(w, http.StatusConflict, "conflict", "tenant "+st)
		return
	}
	devices, err := s.Store.ListDevices(r.Context(), t.ID)
	if err != nil {
		pairingInternal(w, r, err)
		return
	}
	active := 0
	for _, d := range devices {
		if d.RevokedAt == nil {
			active++
		}
	}
	if active >= store.MaxDevices {
		writeAdminError(w, http.StatusConflict, "too_many_pending", "the tenant already has 50 devices")
		return
	}
	p, code, err := s.Store.CreatePairing(r.Context(), t.ID, in.Label, ttl, now)
	switch {
	case errors.Is(err, store.ErrPendingLimit), errors.Is(err, store.ErrDeviceLimit):
		writeAdminError(w, http.StatusConflict, "too_many_pending", "the tenant already has 5 unused pairing codes")
		return
	case err != nil:
		adminStoreError(w, r, err)
		return
	}
	base := s.publicURL()
	writeJSON(w, http.StatusCreated, map[string]string{
		"pairing_id": p.ID.String(),
		"code":       code, // shown once: only its hash is stored
		"expires_at": p.ExpiresAt.UTC().Format(pairingRespLayout),
		"url":        base,
		"pair_uri":   "qoimeks://pair?url=" + url.QueryEscape(base) + "&pair=" + url.QueryEscape(code),
	})
}

func (s *Server) cancelPairing(w http.ResponseWriter, r *http.Request) {
	t, ok := s.pairingTenant(w, r)
	if !ok {
		return
	}
	pid, err := uuid.Parse(r.PathValue("pairing_id"))
	if err != nil {
		notFound(w, "pairing")
		return
	}
	at := s.clock()
	err = s.Store.CancelPairing(r.Context(), t.ID, pid, at)
	if errors.Is(err, store.ErrNotFound) {
		// A repeat answers 200 with the first cancel time; used or unknown codes are 404.
		p, perr := s.Store.PairingByID(r.Context(), t.ID, pid)
		if perr != nil || p.CancelledAt == nil || p.UsedAt != nil {
			if perr != nil && !errors.Is(perr, store.ErrNotFound) {
				pairingInternal(w, r, perr)
				return
			}
			notFound(w, "pairing")
			return
		}
		at, err = *p.CancelledAt, nil
	}
	if err != nil {
		pairingInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"pairing_id":   pid.String(),
		"cancelled_at": at.UTC().Format(pairingRespLayout),
	})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	t, ok := s.pairingTenant(w, r)
	if !ok {
		return
	}
	devices, err := s.Store.ListDevices(r.Context(), t.ID)
	if err != nil {
		pairingInternal(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	t, ok := s.pairingTenant(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(r.PathValue("device_id"))
	if err != nil {
		notFound(w, "device")
		return
	}
	switch err := s.Store.RevokeDevice(r.Context(), t.ID, did, s.clock()); {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "device")
		return
	case err != nil:
		pairingInternal(w, r, err)
		return
	}
	d, err := s.Store.DeviceOf(r.Context(), t.ID, did)
	if err != nil {
		pairingInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceJSON(d))
}
