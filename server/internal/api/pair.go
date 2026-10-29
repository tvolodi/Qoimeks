package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"qoimeks/server/internal/store"
)

// Limits of the public exchange (docs/pairing-design.md, section 4).
var pairExchangeLimits = failureLimits{
	Window:       10 * time.Minute,
	KeyAttempts:  30,
	KeyFailures:  10,
	GlobalFailed: 300,
}

const pairMaxBody = 4 << 10

// pairRoutes registers the public pairing exchange. Like the admin API it exists only when
// a ProvisioningToken is set. It is outside /api/, so requireToken does not guard it: the
// caller has no credential yet.
func (s *Server) pairRoutes(mux *http.ServeMux) {
	if s.ProvisioningToken == "" {
		return
	}
	limiter := newKeyedLimiter(pairExchangeLimits, s.clock)
	mux.HandleFunc("POST /pair/v1/exchange", func(w http.ResponseWriter, r *http.Request) {
		s.pairExchange(w, r, limiter)
	})
}

func (s *Server) pairExchange(w http.ResponseWriter, r *http.Request, limiter *keyedLimiter) {
	w.Header().Set("Cache-Control", "no-store")
	ip := clientIP(r, s.TrustedProxies)
	if ok, wait := limiter.allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, pairMaxBody)
	var in struct {
		Code        string `json:"code"`
		DeviceLabel string `json:"device_label"`
		Operator    string `json:"operator"`
		AppVersion  string `json:"app_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	switch {
	case in.Code == "" || in.DeviceLabel == "":
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "code and device_label are required")
		return
	case len([]rune(in.DeviceLabel)) > pairingMaxLabel, len([]rune(in.Operator)) > pairingMaxLabel, len(in.AppVersion) > pairingMaxVersion:
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "device_label, operator or app_version is too long")
		return
	}
	d, key, err := s.Store.ExchangePairing(r.Context(), in.Code, store.DeviceInfo{
		Label: in.DeviceLabel, Operator: in.Operator, AppVersion: in.AppVersion,
	}, s.clock())
	switch {
	case errors.Is(err, store.ErrPairingCodeInvalid):
		limiter.fail(ip)
		writeAdminError(w, http.StatusBadRequest, "invalid_pairing_code", "the pairing code is wrong, used or expired")
		return
	case errors.Is(err, store.ErrTenantUnavailable):
		writeAdminError(w, http.StatusForbidden, "tenant_unavailable", "the tenant is suspended or expired")
		return
	case errors.Is(err, store.ErrDeviceLimit):
		writeAdminError(w, http.StatusConflict, "too_many_devices", "the tenant already has 50 devices")
		return
	case err != nil:
		slog.Error("pairing exchange failed", "err", err)
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	t, err := s.Store.Tenant(r.Context(), d.TenantID)
	if err != nil {
		slog.Error("pairing exchange: tenant lookup failed", "err", err)
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":  d.ID.String(),
		"device_key": key, // shown once: only its hash is stored
		"tenant":     map[string]string{"name": t.Name, "language": t.Language},
	})
}
