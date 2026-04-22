package actions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// PermissionChecker checks whether a user holds a given permission key.
type PermissionChecker interface {
	HasPermission(ctx context.Context, userID string, key string) (bool, error)
}

// AuditEntryInput is the data written to the audit sink on each action attempt.
type AuditEntryInput struct {
	ActionType        string
	ActorUserID       string
	ActorRoleSnapshot []string
	ResourceType      string
	ResourceID        string
	TargetUserID      *string
	Params            json.RawMessage
	Reason            *string
	Result            string
	ErrorMessage      *string
	IPAddress         *string
	UserAgent         *string
}

// AuditSink persists audit entries.
type AuditSink interface {
	RecordEntry(ctx context.Context, e AuditEntryInput) error
}

// ActorResolver extracts the acting user's identity from the request.
// Returns the user ID (TEXT app_users.id), their current role snapshot, and any error.
type ActorResolver func(r *http.Request) (userID string, roles []string, err error)

// Dispatcher is an http.Handler that routes POST /…/:type to the matching
// AdminAction after enforcing RBAC, reason, dangerous-confirmation, validation,
// and audit logging.
type Dispatcher struct {
	registry *Registry
	perms    PermissionChecker
	audit    AuditSink
	actor    ActorResolver
}

// NewDispatcher constructs a Dispatcher. reg, perms, audit and actor must all
// be non-nil.
func NewDispatcher(reg *Registry, perms PermissionChecker, audit AuditSink, actor ActorResolver) *Dispatcher {
	return &Dispatcher{registry: reg, perms: perms, audit: audit, actor: actor}
}

// requestBody is the expected JSON envelope posted by the client.
type requestBody struct {
	Params            json.RawMessage `json:"params"`
	Reason            string          `json:"reason"`
	TypedConfirmation string          `json:"typedConfirmation"`
}

// errResponse is returned for any non-2xx response.
type errResponse struct {
	Error    string `json:"error"`
	Message  string `json:"message,omitempty"`
	Expected string `json:"expected,omitempty"`
	Required string `json:"required,omitempty"`
}

// ServeHTTP implements http.Handler. It expects a chi router param "type".
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Resolve action from registry.
	actionType := chi.URLParam(r, "type")
	action, ok := d.registry.Get(actionType)
	if !ok {
		writeJSON(w, http.StatusNotFound, errResponse{Error: "action_not_found"})
		return
	}

	// 2. Resolve actor identity.
	actorID, roles, err := d.actor(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errResponse{Error: "unauthenticated"})
		return
	}

	// 3. Permission check.
	allowed, err := d.perms.HasPermission(ctx, actorID, action.Permission())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResponse{Error: "permission_check_failed"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, errResponse{Error: "permission_denied", Required: action.Permission()})
		return
	}

	// 4. Decode request body. A missing or empty body is treated as an empty
	//    params envelope — validation will reject it if params are required.
	var body requestBody
	if decErr := json.NewDecoder(r.Body).Decode(&body); decErr != nil {
		// Ignore EOF (empty body); any other decode error is a bad request.
		if !errors.Is(decErr, io.EOF) {
			writeJSON(w, http.StatusBadRequest, errResponse{Error: "invalid_json", Message: decErr.Error()})
			return
		}
	}
	if body.Params == nil {
		body.Params = json.RawMessage("{}")
	}

	// 5. Reason enforcement.
	if action.RequiresReason() && body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "reason_required"})
		return
	}

	// 6. Validate params.
	validated, err := action.Validate(body.Params)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "validation_failed", Message: err.Error()})
		return
	}

	// 7. Dangerous confirmation check.
	if action.Dangerous() && body.TypedConfirmation != validated.ConfirmationExpected {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "confirmation_mismatch", Expected: validated.ConfirmationExpected})
		return
	}

	// 8. Redact sensitive params before audit storage.
	redacted, err := RedactParams(body.Params, action.SensitiveParamKeys())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResponse{Error: "redaction_failed", Message: err.Error()})
		return
	}

	// 9. Build ActionInput and execute.
	input := ActionInput{
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		Params:            body.Params,
		Validated:         validated,
		Reason:            body.Reason,
		TypedConfirmation: body.TypedConfirmation,
	}

	result, execErr := action.Execute(ctx, input)

	// 10. Build the base audit entry (fields shared by success and error paths).
	ip := clientIP(r)
	ua := r.UserAgent()
	entry := AuditEntryInput{
		ActionType:        action.Type(),
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		ResourceType:      action.ResourceType(),
		ResourceID:        validated.ResourceID,
		TargetUserID:      validated.TargetUserID,
		Params:            redacted,
		Reason:            optionalString(body.Reason),
		IPAddress:         optionalString(ip),
		UserAgent:         optionalString(ua),
	}

	// 11a. Execution error — write audit with a separate background context so
	//      the audit write is not tied to the (potentially already-cancelled)
	//      request context. Return 500.
	if execErr != nil {
		msg := execErr.Error()
		entry.Result = "error"
		entry.ErrorMessage = &msg
		_ = d.audit.RecordEntry(context.Background(), entry)
		status, code := mapExecError(execErr)
		writeJSON(w, status, errResponse{Error: code, Message: msg})
		return
	}

	// 11b. Success — allow the Execute result to override resource identifiers,
	//      write audit in the request context, return 200.
	entry.Result = "success"
	if result.TargetUserID != nil {
		entry.TargetUserID = result.TargetUserID
	}
	if result.ResourceID != "" {
		entry.ResourceID = result.ResourceID
	}
	_ = d.audit.RecordEntry(ctx, entry)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result.Payload})
}

// mapExecError maps action sentinel errors to HTTP status codes and error codes.
func mapExecError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, ErrPermissionDenied):
		return http.StatusForbidden, "permission_denied"
	case errors.Is(err, ErrValidation):
		return http.StatusBadRequest, "validation_failed"
	default:
		return http.StatusInternalServerError, "execution_failed"
	}
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// optionalString returns nil if s is empty, otherwise a pointer to s.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// clientIP extracts the best-guess client IP from the request headers.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	// http.Request.RemoteAddr is "host:port"; the `inet` column only accepts
	// bare IPs, so strip the port if present.
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
