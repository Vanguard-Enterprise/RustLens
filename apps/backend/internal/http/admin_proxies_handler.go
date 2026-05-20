package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/actions"
	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/clientip"
	"rustlens/backend/internal/store"
)

type adminProxyCreateRequest struct {
	Endpoint string `json:"endpoint"`
	Reason   string `json:"reason,omitempty"`
}

type adminProxyUpdateRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

type adminProxyBulkRequest struct {
	Action  string  `json:"action"`
	IDs     []int64 `json:"ids"`
	Enabled *bool   `json:"enabled,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

type adminProxyBulkResultItem struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func proxyStatus(proxy store.OutboundProxy) string {
	if proxy.AutoDisabledAt != nil {
		return "broken"
	}
	if proxy.Enabled {
		return "active"
	}
	return "paused"
}

func adminProxyToMap(proxy store.OutboundProxy) map[string]any {
	m := map[string]any{
		"id":        proxy.ID,
		"endpoint":  proxy.RedactedEndpoint,
		"enabled":   proxy.Enabled,
		"status":    proxyStatus(proxy),
		"createdAt": proxy.CreatedAt,
		"updatedAt": proxy.UpdatedAt,
	}
	if proxy.AutoDisabledAt != nil {
		m["autoDisabledAt"] = *proxy.AutoDisabledAt
	}
	return m
}

// requireProxyPermission checks the actor has the given permission via the
// live rbac resolver. Writes a 401/403 and returns false on failure.
func (h *Handler) requireProxyPermission(w http.ResponseWriter, r *http.Request, perm string) (string, bool) {
	// Resolve actor from either an API key or a session. The previous version
	// only consulted appauth.SessionFromContext, so API keys with the right
	// scope could not reach proxy management — defeating the API-key model.
	var userID string
	if k := actions.APIKeyAuthFromContext(r.Context()); k != nil {
		userID = k.UserID
	} else if s := appauth.SessionFromContext(r.Context()); s != nil {
		userID = s.UserID
	}
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "session_required")
		return "", false
	}
	if h.rbacResolver == nil {
		writeError(w, http.StatusInternalServerError, "rbac unavailable")
		return "", false
	}
	allowed, err := h.rbacResolver.HasPermission(r.Context(), userID, perm)
	if err != nil {
		log.Printf("[backend] admin proxies permission check error: %v", err)
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return "", false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "permission_denied")
		return "", false
	}
	return userID, true
}

func (h *Handler) recordProxyAudit(ctx context.Context, r *http.Request, actorID, actionType, resourceID, reason, result, errMsg string) {
	if h.auditStore == nil {
		return
	}
	entry := audit.Entry{
		ActionType:   actionType,
		ActorUserID:  &actorID,
		ResourceType: "proxy",
		ResourceID:   resourceID,
		Result:       result,
	}
	if reason != "" {
		entry.Reason = &reason
	}
	if errMsg != "" {
		entry.ErrorMessage = &errMsg
	}
	if ip := h.clientIP(r); ip != "" {
		entry.IPAddress = &ip
	}
	if ua := r.UserAgent(); ua != "" {
		entry.UserAgent = &ua
	}
	if err := h.auditStore.Record(ctx, entry); err != nil {
		log.Printf("[backend] admin proxies audit record failed: %v", err)
	}
}

// clientIP returns the actor IP for audit entries, honoring the
// TRUST_PROXY_HEADERS / TRUST_PROXY_IP_HEADER configuration.
func (h *Handler) clientIP(r *http.Request) string {
	return clientip.Extract(r, clientip.Headers{
		Trust:         h.cfg.TrustProxyHeaders,
		IPHeader:      h.cfg.TrustProxyIPHeader,
		CountryHeader: h.cfg.TrustProxyGeoHeader,
	})
}

// ListAdminProxies returns a paginated, optionally-filtered slice of outbound
// proxies. Broken (auto-disabled) proxies are pinned to the top. Gated by
// proxies.read.
func (h *Handler) ListAdminProxies(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireProxyPermission(w, r, rbac.PermProxiesRead); !ok {
		return
	}

	q := r.URL.Query()
	page := 1
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 50
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			pageSize = n
		}
	}
	filter := store.OutboundProxyFilter{
		Q:      q.Get("q"),
		Status: q.Get("status"),
	}
	if filter.Status != "" && filter.Status != "active" && filter.Status != "paused" && filter.Status != "broken" {
		filter.Status = ""
	}

	offset := (page - 1) * pageSize
	proxies, total, err := h.postgres.ListOutboundProxiesPage(r.Context(), filter, pageSize, offset)
	if err != nil {
		log.Printf("[backend] ListAdminProxies error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list proxies")
		return
	}

	items := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		items = append(items, adminProxyToMap(proxy))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// CreateAdminProxy validates and stores a new outbound proxy. Gated by
// proxies.edit.
func (h *Handler) CreateAdminProxy(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireProxyPermission(w, r, rbac.PermProxiesEdit)
	if !ok {
		return
	}
	if h.proxyCrypto == nil {
		writeError(w, http.StatusServiceUnavailable, "PROXY_ENCRYPTION_KEY is required before database proxies can be managed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request adminProxyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	endpoint, redactedEndpoint, err := validateProxyEndpoint(request.Endpoint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ciphertext, nonce, err := h.proxyCrypto.Encrypt(endpoint)
	if err != nil {
		log.Printf("[backend] CreateAdminProxy encrypt error for %q: %v", redactedEndpoint, err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.create", redactedEndpoint, request.Reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to encrypt proxy")
		return
	}

	proxy, err := h.postgres.CreateOutboundProxy(r.Context(), redactedEndpoint, ciphertext, nonce, "default")
	if errors.Is(err, store.ErrOutboundProxyAlreadyExists) {
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.create", redactedEndpoint, request.Reason, "error", "already_exists")
		writeError(w, http.StatusConflict, "proxy already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateAdminProxy store error for %q: %v", redactedEndpoint, err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.create", redactedEndpoint, request.Reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to store proxy")
		return
	}
	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] CreateAdminProxy reload error: %v", err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.create", strconv.FormatInt(proxy.ID, 10), request.Reason, "error", "reload_failed")
		writeError(w, http.StatusInternalServerError, "proxy saved but reload failed")
		return
	}

	h.recordProxyAudit(r.Context(), r, actorID, "proxies.create", strconv.FormatInt(proxy.ID, 10), request.Reason, "success", "")
	writeJSON(w, http.StatusCreated, adminProxyToMap(proxy))
}

// UpdateAdminProxy toggles the enabled flag on a proxy. Gated by proxies.edit.
func (h *Handler) UpdateAdminProxy(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireProxyPermission(w, r, rbac.PermProxiesEdit)
	if !ok {
		return
	}
	proxyID, err := strconv.ParseInt(chi.URLParam(r, "proxyID"), 10, 64)
	if err != nil || proxyID <= 0 {
		writeError(w, http.StatusBadRequest, "proxyID must be a positive integer")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request adminProxyUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	proxy, err := h.postgres.SetOutboundProxyEnabled(r.Context(), proxyID, request.Enabled)
	if errors.Is(err, store.ErrOutboundProxyNotFound) {
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.update", strconv.FormatInt(proxyID, 10), request.Reason, "error", "not_found")
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateAdminProxy store error for %d: %v", proxyID, err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.update", strconv.FormatInt(proxyID, 10), request.Reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to update proxy")
		return
	}
	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] UpdateAdminProxy reload error: %v", err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.update", strconv.FormatInt(proxyID, 10), request.Reason, "error", "reload_failed")
		writeError(w, http.StatusInternalServerError, "proxy updated but reload failed")
		return
	}

	h.recordProxyAudit(r.Context(), r, actorID, "proxies.update", strconv.FormatInt(proxyID, 10), request.Reason, "success", "")
	writeJSON(w, http.StatusOK, adminProxyToMap(proxy))
}

// DeleteAdminProxy removes a proxy entirely. Gated by proxies.edit.
func (h *Handler) DeleteAdminProxy(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireProxyPermission(w, r, rbac.PermProxiesEdit)
	if !ok {
		return
	}
	proxyID, err := strconv.ParseInt(chi.URLParam(r, "proxyID"), 10, 64)
	if err != nil || proxyID <= 0 {
		writeError(w, http.StatusBadRequest, "proxyID must be a positive integer")
		return
	}

	reason := r.URL.Query().Get("reason")

	if err := h.postgres.DeleteOutboundProxy(r.Context(), proxyID); errors.Is(err, store.ErrOutboundProxyNotFound) {
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.delete", strconv.FormatInt(proxyID, 10), reason, "error", "not_found")
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteAdminProxy store error for %d: %v", proxyID, err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.delete", strconv.FormatInt(proxyID, 10), reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to delete proxy")
		return
	}
	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] DeleteAdminProxy reload error: %v", err)
		h.recordProxyAudit(r.Context(), r, actorID, "proxies.delete", strconv.FormatInt(proxyID, 10), reason, "error", "reload_failed")
		writeError(w, http.StatusInternalServerError, "proxy deleted but reload failed")
		return
	}

	h.recordProxyAudit(r.Context(), r, actorID, "proxies.delete", strconv.FormatInt(proxyID, 10), reason, "success", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// BulkAdminProxies handles bulk enable/disable/delete operations. Gated by
// proxies.edit.
func (h *Handler) BulkAdminProxies(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireProxyPermission(w, r, rbac.PermProxiesEdit)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request adminProxyBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(request.IDs) == 0 || len(request.IDs) > 200 {
		writeError(w, http.StatusBadRequest, "ids must be 1..200")
		return
	}

	results := make([]adminProxyBulkResultItem, 0, len(request.IDs))
	succeeded, failed := 0, 0

	switch request.Action {
	case "enable", "disable":
		desired := request.Action == "enable"
		if request.Enabled != nil {
			desired = *request.Enabled
		}
		for _, id := range request.IDs {
			res := adminProxyBulkResultItem{ID: id}
			_, err := h.postgres.SetOutboundProxyEnabled(r.Context(), id, desired)
			if errors.Is(err, store.ErrOutboundProxyNotFound) {
				res.Error = "not_found"
				failed++
			} else if err != nil {
				log.Printf("[backend] BulkAdminProxies set enabled %d: %v", id, err)
				res.Error = "store_error"
				failed++
			} else {
				res.OK = true
				succeeded++
			}
			h.recordProxyAudit(r.Context(), r, actorID, "proxies.bulk."+request.Action, strconv.FormatInt(id, 10), request.Reason, resultOf(res.OK), res.Error)
			results = append(results, res)
		}
	case "delete":
		for _, id := range request.IDs {
			res := adminProxyBulkResultItem{ID: id}
			err := h.postgres.DeleteOutboundProxy(r.Context(), id)
			if errors.Is(err, store.ErrOutboundProxyNotFound) {
				res.Error = "not_found"
				failed++
			} else if err != nil {
				log.Printf("[backend] BulkAdminProxies delete %d: %v", id, err)
				res.Error = "store_error"
				failed++
			} else {
				res.OK = true
				succeeded++
			}
			h.recordProxyAudit(r.Context(), r, actorID, "proxies.bulk.delete", strconv.FormatInt(id, 10), request.Reason, resultOf(res.OK), res.Error)
			results = append(results, res)
		}
	case "clear_broken":
		for _, id := range request.IDs {
			res := adminProxyBulkResultItem{ID: id}
			_, err := h.postgres.ClearOutboundProxyAutoDisabled(r.Context(), id)
			if errors.Is(err, store.ErrOutboundProxyNotFound) {
				res.Error = "not_found"
				failed++
			} else if err != nil {
				log.Printf("[backend] BulkAdminProxies clear_broken %d: %v", id, err)
				res.Error = "store_error"
				failed++
			} else {
				res.OK = true
				succeeded++
			}
			h.recordProxyAudit(r.Context(), r, actorID, "proxies.bulk.clear_broken", strconv.FormatInt(id, 10), request.Reason, resultOf(res.OK), res.Error)
			results = append(results, res)
		}
	default:
		writeError(w, http.StatusBadRequest, "action must be one of: enable, disable, delete, clear_broken")
		return
	}

	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] BulkAdminProxies reload error: %v", err)
		writeError(w, http.StatusInternalServerError, "bulk applied but reload failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"action":    request.Action,
		"succeeded": succeeded,
		"failed":    failed,
		"results":   results,
	})
}

func resultOf(ok bool) string {
	if ok {
		return "success"
	}
	return "error"
}
