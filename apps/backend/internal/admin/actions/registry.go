package actions

import "fmt"

type PermissionValidator func(key string) bool

type Registry struct {
	actions    map[string]AdminAction
	isValidPerm PermissionValidator
}

func NewRegistry(isValidPerm PermissionValidator) *Registry {
	return &Registry{actions: map[string]AdminAction{}, isValidPerm: isValidPerm}
}

func (r *Registry) Register(a AdminAction) error {
	if _, exists := r.actions[a.Type()]; exists {
		return fmt.Errorf("action %q already registered", a.Type())
	}
	// Superadmin-only and bulk-wrapper actions may leave Permission() empty —
	// they bypass the key check at dispatch time (superadmin-only falls through
	// to role check; bulk wrappers defer permission checks to sub-actions).
	sa, saOK := a.(interface{ SuperadminOnly() bool })
	skipped, skipOK := a.(interface{ PermissionSkipped() bool })
	if !((saOK && sa.SuperadminOnly() && a.Permission() == "") ||
		(skipOK && skipped.PermissionSkipped() && a.Permission() == "")) {
		if !r.isValidPerm(a.Permission()) {
			return fmt.Errorf("action %q requires unknown permission %q", a.Type(), a.Permission())
		}
	}
	r.actions[a.Type()] = a
	return nil
}

func (r *Registry) Get(t string) (AdminAction, bool) {
	a, ok := r.actions[t]
	return a, ok
}

func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.actions))
	for k := range r.actions {
		out = append(out, k)
	}
	return out
}

func (r *Registry) All() []AdminAction {
	out := make([]AdminAction, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a)
	}
	return out
}
