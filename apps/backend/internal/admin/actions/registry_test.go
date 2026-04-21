package actions

import "testing"

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry(fakePermCheck)
	if err := r.Register(fakeAction{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("test.noop")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if got.Type() != "test.noop" {
		t.Errorf("Type mismatch")
	}
}

func TestRegistry_RejectsDuplicate(t *testing.T) {
	r := NewRegistry(fakePermCheck)
	_ = r.Register(fakeAction{})
	err := r.Register(fakeAction{})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistry_RejectsUnknownPermission(t *testing.T) {
	r := NewRegistry(func(key string) bool { return false })
	err := r.Register(fakeAction{})
	if err == nil {
		t.Fatal("expected unknown permission error")
	}
}

func fakePermCheck(key string) bool { return key == "audit.read" }
