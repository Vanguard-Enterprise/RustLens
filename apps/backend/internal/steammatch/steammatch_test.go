package steammatch

import (
	"context"
	"errors"
	"testing"
)

type fakeResolver struct {
	bm  string
	err error
}

func (f fakeResolver) SteamToBM(context.Context, string) (string, error) { return f.bm, f.err }
func (f fakeResolver) BMToSteam(context.Context, string) (string, error) { return f.bm, f.err }

func TestChain_PrimaryHit(t *testing.T) {
	c := NewChain(fakeResolver{bm: "111"}, fakeResolver{bm: "999"})
	got, err := c.SteamToBM(context.Background(), "s")
	if err != nil || got != "111" {
		t.Fatalf("want 111,nil got %q,%v", got, err)
	}
}

func TestChain_FallsThroughNotFoundToFallback(t *testing.T) {
	c := NewChain(fakeResolver{err: ErrNotFound}, fakeResolver{bm: "222"})
	got, err := c.SteamToBM(context.Background(), "s")
	if err != nil || got != "222" {
		t.Fatalf("want 222,nil got %q,%v", got, err)
	}
}

func TestChain_PendingPropagatesWhenNoneResolve(t *testing.T) {
	c := NewChain(fakeResolver{err: ErrPending}, fakeResolver{err: ErrNotFound})
	_, err := c.SteamToBM(context.Background(), "s")
	if !errors.Is(err, ErrPending) {
		t.Fatalf("want ErrPending, got %v", err)
	}
}

func TestChain_PendingButFallbackResolves(t *testing.T) {
	c := NewChain(fakeResolver{err: ErrPending}, fakeResolver{bm: "333"})
	got, err := c.SteamToBM(context.Background(), "s")
	if err != nil || got != "333" {
		t.Fatalf("want 333,nil got %q,%v", got, err)
	}
}

func TestChain_AllNotConfigured(t *testing.T) {
	c := NewChain(fakeResolver{err: ErrNotConfigured})
	_, err := c.SteamToBM(context.Background(), "s")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestChain_HardErrorFallsThroughThenNotFound(t *testing.T) {
	c := NewChain(fakeResolver{err: errors.New("boom")}, fakeResolver{err: ErrNotFound})
	_, err := c.SteamToBM(context.Background(), "s")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestTranslating_MapsSentinels(t *testing.T) {
	localNotFound := errors.New("local: not found")
	tr := Translating{Inner: fakeResolver{err: localNotFound}, NotFound: localNotFound}
	_, err := tr.SteamToBM(context.Background(), "s")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want canonical ErrNotFound, got %v", err)
	}
}
