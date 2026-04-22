package actions

import "errors"

// Action error sentinels. Execute() may wrap these with fmt.Errorf("%w ...")
// to attach context; the dispatcher uses errors.Is to pick the HTTP status.

var (
	// ErrNotFound maps to 404.
	ErrNotFound = errors.New("not_found")
	// ErrConflict maps to 409 (e.g. soft-deleted user, last-superadmin guard).
	ErrConflict = errors.New("conflict")
	// ErrPermissionDenied maps to 403 (extra per-param permission gates).
	ErrPermissionDenied = errors.New("permission_denied")
	// ErrValidation maps to 400 (rare — usually Validate() catches these first).
	ErrValidation = errors.New("validation_failed")
)
