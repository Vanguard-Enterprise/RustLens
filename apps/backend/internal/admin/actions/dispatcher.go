package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/clientip"
)

// PermissionChecker checks whether a user holds a given permission key.
type PermissionChecker interface {
	HasPermission(ctx context.Context, userID string, key string) (bool, error)
}

// permissionsForUserer is an optional extension used by superadmin-only
// actions. The real rbac.Resolver satisfies it.
type permissionsForUserer interface {
	PermissionsForUser(ctx context.Context, userID string) ([]string, error)
}

// permsFor loads the actor's permission set as a map. Falls back to a single
// "admins.grant" probe if the checker doesn't implement PermissionsForUser.
func (d *Dispatcher) permsFor(ctx context.Context, userID string) (map[string]bool, error) {
	if pu, ok := d.perms.(permissionsForUserer); ok {
		keys, err := pu.PermissionsForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(keys))
		for _, k := range keys {
			out[k] = true
		}
		return out, nil
	}
	has, err := d.perms.HasPermission(ctx, userID, "admins.grant")
	if err != nil {
		return nil, err
	}
	return map[string]bool{"admins.grant": has}, nil
}

func containsRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
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
	registry    *Registry
	perms       PermissionChecker
	audit       AuditSink
	actor       ActorResolver
	rateLimiter *rateLimiter
	idemCache   *idempotencyCache
	ipHeaders   clientip.Headers
}

// SetClientIPHeaders configures which request headers the dispatcher reads
// when capturing the actor IP for audit entries. Defaults to RemoteAddr-only.
func (d *Dispatcher) SetClientIPHeaders(h clientip.Headers) {
	d.ipHeaders = h
}

// NewDispatcher constructs a Dispatcher. reg, perms, audit and actor must all
// be non-nil.
func NewDispatcher(reg *Registry, perms PermissionChecker, audit AuditSink, actor ActorResolver) *Dispatcher {
	return &Dispatcher{
		registry:    reg,
		perms:       perms,
		audit:       audit,
		actor:       actor,
		rateLimiter: newRateLimiter(),
		idemCache:   newIdempotencyCache(),
	}
}

// Dispatch executes an action inline (without going through ServeHTTP). It is
// used by wrapper actions such as users.bulk that need to invoke sub-actions
// while still honouring the dispatcher's permission check and audit pipeline.
//
// Rate-limiting and idempotency are intentionally NOT applied here; sub-action
// invocations share the top-level caller's rate budget.
func (d *Dispatcher) Dispatch(ctx context.Context, actorID string, roles []string, actionType string, params json.RawMessage, reason string) (any, error) {
	action, ok := d.registry.Get(actionType)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, actionType)
	}
	// Permission check (mirrors ServeHTTP).
	if skip, ok := action.(interface{ PermissionSkipped() bool }); ok && skip.PermissionSkipped() {
		// no-op; wrapper actions handle permissions internally
	} else if sa, ok := action.(interface{ SuperadminOnly() bool }); ok && sa.SuperadminOnly() {
		perms, err := d.permsFor(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if !perms["admins.grant"] && !containsRole(roles, "superadmin") {
			return nil, fmt.Errorf("%w: superadmin required", ErrPermissionDenied)
		}
	} else {
		allowed, err := d.perms.HasPermission(ctx, actorID, action.Permission())
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, action.Permission())
		}
	}
	if params == nil {
		params = json.RawMessage("{}")
	}
	validated, err := action.Validate(params)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	input := ActionInput{
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		Params:            params,
		Validated:         validated,
		Reason:            reason,
	}
	result, execErr := action.Execute(ctx, input)
	redacted, _ := RedactParams(params, action.SensitiveParamKeys())
	entry := AuditEntryInput{
		ActionType:        action.Type(),
		ActorUserID:       actorID,
		ActorRoleSnapshot: roles,
		ResourceType:      action.ResourceType(),
		ResourceID:        validated.ResourceID,
		TargetUserID:      validated.TargetUserID,
		Params:            redacted,
		Reason:            optionalString(reason),
	}
	if execErr != nil {
		msg := execErr.Error()
		entry.Result = "error"
		entry.ErrorMessage = &msg
		_ = d.audit.RecordEntry(context.Background(), entry)
		return nil, execErr
	}
	entry.Result = "success"
	if result.TargetUserID != nil {
		entry.TargetUserID = result.TargetUserID
	}
	if result.ResourceID != "" {
		entry.ResourceID = result.ResourceID
	}
	_ = d.audit.RecordEntry(ctx, entry)
	return result.Payload, nil
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

	// 2a. Rate limit: per-actor, per-action token bucket.
	capacity, refill := bucketParams(action)
	if ok, retryAfter := d.rateLimiter.Allow(actorID, actionType, capacity, refill); !ok {
		secs := int(retryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSON(w, http.StatusTooManyRequests, errResponse{Error: "rate_limited"})
		return
	}

	// 2b. Idempotency cache: if the client supplied Idempotency-Key header and
	//     a prior response is still fresh, replay it verbatim without executing
	//     the action again.
	idemKey := r.Header.Get("Idempotency-Key")
	var idemCacheKey string
	if idemKey != "" {
		idemCacheKey = d.idemCache.key(actorID, actionType, idemKey)
		if cached, ok := d.idemCache.get(idemCacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotent-Replay", "true")
			w.WriteHeader(cached.status)
			_, _ = w.Write(cached.body)
			return
		}
	}

	// Buffer the response so we can cache the body for idempotency replay.
	var rw http.ResponseWriter = w
	var buf *recordingResponseWriter
	if idemCacheKey != "" {
		buf = &recordingResponseWriter{ResponseWriter: w}
		rw = buf
	}

	// 3. Permission check. Superadmin-only actions bypass the permission-key
	//    check and instead require the actor to hold the 'superadmin' role
	//    (resolved through the real permissions resolver which treats
	//    superadmin as implicit-all). Wrapper actions that mark themselves as
	//    PermissionSkipped skip the pre-check entirely (they perform per-row
	//    checks in Execute via the dispatcher's internal Dispatch helper).
	if skip, ok := action.(interface{ PermissionSkipped() bool }); ok && skip.PermissionSkipped() {
		// no-op
	} else if sa, ok := action.(interface{ SuperadminOnly() bool }); ok && sa.SuperadminOnly() {
		perms, err := d.permsFor(ctx, actorID)
		if err != nil {
			writeJSON(rw, http.StatusInternalServerError, errResponse{Error: "permission_check_failed"})
			return
		}
		if !perms["admins.grant"] && !containsRole(roles, "superadmin") {
			writeJSON(rw, http.StatusForbidden, errResponse{Error: "permission_denied", Required: "superadmin"})
			return
		}
	} else {
		allowed, err := d.perms.HasPermission(ctx, actorID, action.Permission())
		if err != nil {
			writeJSON(rw, http.StatusInternalServerError, errResponse{Error: "permission_check_failed"})
			return
		}
		if !allowed {
			writeJSON(rw, http.StatusForbidden, errResponse{Error: "permission_denied", Required: action.Permission()})
			return
		}
	}

	// 4. Decode request body. A missing or empty body is treated as an empty
	//    params envelope — validation will reject it if params are required.
	var body requestBody
	if decErr := json.NewDecoder(r.Body).Decode(&body); decErr != nil {
		// Ignore EOF (empty body); any other decode error is a bad request.
		if !errors.Is(decErr, io.EOF) {
			writeJSON(rw, http.StatusBadRequest, errResponse{Error: "invalid_json", Message: decErr.Error()})
			return
		}
	}
	if body.Params == nil {
		body.Params = json.RawMessage("{}")
	}

	// 5. Reason enforcement.
	if action.RequiresReason() && body.Reason == "" {
		writeJSON(rw, http.StatusBadRequest, errResponse{Error: "reason_required"})
		return
	}

	// 6. Validate params.
	validated, err := action.Validate(body.Params)
	if err != nil {
		writeJSON(rw, http.StatusBadRequest, errResponse{Error: "validation_failed", Message: err.Error()})
		return
	}

	// 7. Dangerous confirmation check.
	if action.Dangerous() && body.TypedConfirmation != validated.ConfirmationExpected {
		writeJSON(rw, http.StatusBadRequest, errResponse{Error: "confirmation_mismatch", Expected: validated.ConfirmationExpected})
		return
	}

	// 8. Redact sensitive params before audit storage.
	redacted, err := RedactParams(body.Params, action.SensitiveParamKeys())
	if err != nil {
		writeJSON(rw, http.StatusBadRequest, errResponse{Error: "redaction_failed", Message: err.Error()})
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
	ip := clientip.Extract(r, d.ipHeaders)
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
		writeJSON(rw, status, errResponse{Error: code, Message: msg})
		// On execution error we still cache the response so retries with the
		// same idempotency key get the same failure instead of re-executing.
		if buf != nil {
			d.idemCache.put(idemCacheKey, buf.status, buf.body.Bytes())
		}
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

	writeJSON(rw, http.StatusOK, map[string]any{"ok": true, "result": result.Payload})
	if buf != nil {
		d.idemCache.put(idemCacheKey, buf.status, buf.body.Bytes())
	}
}

// recordingResponseWriter proxies Write/WriteHeader while buffering the bytes
// so an idempotency cache can replay the response later.
type recordingResponseWriter struct {
	http.ResponseWriter
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (r *recordingResponseWriter) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recordingResponseWriter) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
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

