package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/admin/audit"
	"rustlens/backend/internal/admin/rbac"
	"rustlens/backend/internal/appauth"
	"rustlens/backend/internal/store"
)

type adminBmServerCreateRequest struct {
	BmID          string `json:"bmId"`
	Name          string `json:"name"`
	IsVitality    bool   `json:"isVitality"`
	IsAtlas       bool   `json:"isAtlas"`
	IsVital       bool   `json:"isVital"`
	IsModded      bool   `json:"isModded"`
	IsOfficial    bool   `json:"isOfficial"`
	IsBuild       bool   `json:"isBuild"`
	HighFrequency bool   `json:"highFrequency"`
	Reason        string `json:"reason,omitempty"`
}

type adminBmServerUpdateRequest struct {
	BmID          string `json:"bmId"`
	Name          string `json:"name"`
	IsVitality    bool   `json:"isVitality"`
	IsAtlas       bool   `json:"isAtlas"`
	IsVital       bool   `json:"isVital"`
	IsModded      bool   `json:"isModded"`
	IsOfficial    bool   `json:"isOfficial"`
	IsBuild       bool   `json:"isBuild"`
	HighFrequency bool   `json:"highFrequency"`
	Reason        string `json:"reason,omitempty"`
}

func adminBmServerToMap(s store.TrackedServer) map[string]any {
	m := map[string]any{
		"bmId":          s.BmID,
		"name":          s.Name,
		"isVitality":    s.IsVitality,
		"isAtlas":       s.IsAtlas,
		"isVital":       s.IsVital,
		"isModded":      s.IsModded,
		"isOfficial":    s.IsOfficial,
		"isBuild":       s.IsBuild,
		"highFrequency": s.HighFrequency,
		"lastPollError": s.LastPollError,
	}
	if s.LastPolledAt != nil {
		m["lastPolledAt"] = *s.LastPolledAt
	}
	return m
}

func (h *Handler) requireBmServerPermission(w http.ResponseWriter, r *http.Request, perm string) (string, bool) {
	session := appauth.SessionFromContext(r.Context())
	if session == nil || session.UserID == "" {
		writeError(w, http.StatusUnauthorized, "session_required")
		return "", false
	}
	if h.rbacResolver == nil {
		writeError(w, http.StatusInternalServerError, "rbac unavailable")
		return "", false
	}
	allowed, err := h.rbacResolver.HasPermission(r.Context(), session.UserID, perm)
	if err != nil {
		log.Printf("[backend] admin bm-servers permission check error: %v", err)
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return "", false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "permission_denied")
		return "", false
	}
	return session.UserID, true
}

func (h *Handler) recordBmServerAudit(ctx context.Context, r *http.Request, actorID, actionType, resourceID, reason, result, errMsg string) {
	if h.auditStore == nil {
		return
	}
	entry := audit.Entry{
		ActionType:   actionType,
		ActorUserID:  &actorID,
		ResourceType: "bm_server",
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
		log.Printf("[backend] admin bm-servers audit record failed: %v", err)
	}
}

func parseBoolQuery(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		t := true
		return &t
	case "false", "0", "no":
		f := false
		return &f
	default:
		return nil
	}
}

// ListAdminBmServers: GET /api/app/admin/bm-servers. Gated by bm_servers.read.
func (h *Handler) ListAdminBmServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBmServerPermission(w, r, rbac.PermBmServersRead); !ok {
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

	filter := store.TrackedServerFilter{
		Q:             q.Get("q"),
		HighFrequency: parseBoolQuery(q.Get("highFrequency")),
		HasError:      parseBoolQuery(q.Get("hasError")),
		IsVitality:    parseBoolQuery(q.Get("isVitality")),
		IsAtlas:       parseBoolQuery(q.Get("isAtlas")),
		IsVital:       parseBoolQuery(q.Get("isVital")),
		IsModded:      parseBoolQuery(q.Get("isModded")),
		IsOfficial:    parseBoolQuery(q.Get("isOfficial")),
		IsBuild:       parseBoolQuery(q.Get("isBuild")),
		SortBy:        q.Get("sortBy"),
		SortOrder:     q.Get("sortOrder"),
	}
	switch filter.SortBy {
	case "", "name", "bmId", "highFrequency", "lastPolledAt", "pollStatus":
	default:
		filter.SortBy = ""
	}
	if filter.SortOrder != "asc" && filter.SortOrder != "desc" {
		filter.SortOrder = ""
	}

	offset := (page - 1) * pageSize
	items, total, err := h.postgres.ListTrackedServersPage(r.Context(), filter, pageSize, offset)
	if err != nil {
		log.Printf("[backend] ListAdminBmServers error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list tracked servers")
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, adminBmServerToMap(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func validateBmServerInput(bmID, name string) error {
	bmID = strings.TrimSpace(bmID)
	name = strings.TrimSpace(name)
	if bmID == "" {
		return errors.New("bmId is required")
	}
	if len(bmID) > 64 {
		return errors.New("bmId is too long")
	}
	for _, c := range bmID {
		if c < '0' || c > '9' {
			return errors.New("bmId must be numeric")
		}
	}
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 200 {
		return errors.New("name is too long")
	}
	return nil
}

// CreateAdminBmServer: POST /api/app/admin/bm-servers. Gated by bm_servers.edit.
func (h *Handler) CreateAdminBmServer(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireBmServerPermission(w, r, rbac.PermBmServersEdit)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req adminBmServerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bmID := strings.TrimSpace(req.BmID)
	name := strings.TrimSpace(req.Name)
	if err := validateBmServerInput(bmID, name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	server, err := h.postgres.CreateTrackedServer(
		r.Context(),
		bmID,
		name,
		req.IsVitality,
		req.IsAtlas,
		req.IsVital,
		req.IsModded,
		req.IsOfficial,
		req.IsBuild,
		req.HighFrequency,
	)
	if errors.Is(err, store.ErrTrackedServerAlreadyExists) {
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.create", bmID, req.Reason, "error", "already_exists")
		writeError(w, http.StatusConflict, "tracked server already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateAdminBmServer store error for %q: %v", bmID, err)
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.create", bmID, req.Reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to create tracked server")
		return
	}

	h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.create", bmID, req.Reason, "success", "")
	writeJSON(w, http.StatusCreated, adminBmServerToMap(server))
}

// UpdateAdminBmServer: PUT /api/app/admin/bm-servers/{bmID}. Gated by bm_servers.edit.
func (h *Handler) UpdateAdminBmServer(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireBmServerPermission(w, r, rbac.PermBmServersEdit)
	if !ok {
		return
	}
	currentBmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if currentBmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req adminBmServerUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	nextBmID := strings.TrimSpace(req.BmID)
	if nextBmID == "" {
		nextBmID = currentBmID
	}
	name := strings.TrimSpace(req.Name)
	if err := validateBmServerInput(nextBmID, name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	server, err := h.postgres.UpdateTrackedServer(
		r.Context(),
		currentBmID,
		nextBmID,
		name,
		req.IsVitality,
		req.IsAtlas,
		req.IsVital,
		req.IsModded,
		req.IsOfficial,
		req.IsBuild,
		req.HighFrequency,
	)
	if errors.Is(err, store.ErrTrackedServerNotFound) {
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.update", currentBmID, req.Reason, "error", "not_found")
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	}
	if errors.Is(err, store.ErrTrackedServerAlreadyExists) {
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.update", currentBmID, req.Reason, "error", "already_exists")
		writeError(w, http.StatusConflict, "tracked server with new bmId already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] UpdateAdminBmServer store error for %q: %v", currentBmID, err)
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.update", currentBmID, req.Reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to update tracked server")
		return
	}

	h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.update", server.BmID, req.Reason, "success", "")
	writeJSON(w, http.StatusOK, adminBmServerToMap(server))
}

type adminBmServerBulkRequest struct {
	Action string   `json:"action"`
	BmIDs  []string `json:"bmIds"`
	Reason string   `json:"reason,omitempty"`
}

type adminBmServerBulkResultItem struct {
	BmID  string `json:"bmId"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// BulkAdminBmServers: POST /api/app/admin/bm-servers/bulk. Gated by bm_servers.edit.
// Supported actions: set_high_freq, set_standard, delete.
func (h *Handler) BulkAdminBmServers(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireBmServerPermission(w, r, rbac.PermBmServersEdit)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req adminBmServerBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.BmIDs) == 0 || len(req.BmIDs) > 200 {
		writeError(w, http.StatusBadRequest, "bmIds must be 1..200")
		return
	}

	results := make([]adminBmServerBulkResultItem, 0, len(req.BmIDs))
	succeeded, failed := 0, 0

	switch req.Action {
	case "set_high_freq", "set_standard":
		desired := req.Action == "set_high_freq"
		for _, raw := range req.BmIDs {
			id := strings.TrimSpace(raw)
			res := adminBmServerBulkResultItem{BmID: id}
			if id == "" {
				res.Error = "empty_bmId"
				failed++
				results = append(results, res)
				continue
			}
			err := h.postgres.SetTrackedServerHighFrequency(r.Context(), id, desired)
			if errors.Is(err, store.ErrTrackedServerNotFound) {
				res.Error = "not_found"
				failed++
			} else if err != nil {
				log.Printf("[backend] BulkAdminBmServers set high_frequency %q: %v", id, err)
				res.Error = "store_error"
				failed++
			} else {
				res.OK = true
				succeeded++
			}
			h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.bulk."+req.Action, id, req.Reason, resultOf(res.OK), res.Error)
			results = append(results, res)
		}
	case "delete":
		for _, raw := range req.BmIDs {
			id := strings.TrimSpace(raw)
			res := adminBmServerBulkResultItem{BmID: id}
			if id == "" {
				res.Error = "empty_bmId"
				failed++
				results = append(results, res)
				continue
			}
			err := h.postgres.DeleteTrackedServer(r.Context(), id)
			if errors.Is(err, store.ErrTrackedServerNotFound) {
				res.Error = "not_found"
				failed++
			} else if err != nil {
				log.Printf("[backend] BulkAdminBmServers delete %q: %v", id, err)
				res.Error = "store_error"
				failed++
			} else {
				res.OK = true
				succeeded++
			}
			h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.bulk.delete", id, req.Reason, resultOf(res.OK), res.Error)
			results = append(results, res)
		}
	default:
		writeError(w, http.StatusBadRequest, "action must be one of: set_high_freq, set_standard, delete")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"action":    req.Action,
		"succeeded": succeeded,
		"failed":    failed,
		"results":   results,
	})
}

// DeleteAdminBmServer: DELETE /api/app/admin/bm-servers/{bmID}. Gated by bm_servers.edit.
func (h *Handler) DeleteAdminBmServer(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.requireBmServerPermission(w, r, rbac.PermBmServersEdit)
	if !ok {
		return
	}
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if bmID == "" {
		writeError(w, http.StatusBadRequest, "bmID is required")
		return
	}
	reason := r.URL.Query().Get("reason")

	if err := h.postgres.DeleteTrackedServer(r.Context(), bmID); errors.Is(err, store.ErrTrackedServerNotFound) {
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.delete", bmID, reason, "error", "not_found")
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteAdminBmServer store error for %q: %v", bmID, err)
		h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.delete", bmID, reason, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to delete tracked server")
		return
	}

	h.recordBmServerAudit(r.Context(), r, actorID, "bm_servers.delete", bmID, reason, "success", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
