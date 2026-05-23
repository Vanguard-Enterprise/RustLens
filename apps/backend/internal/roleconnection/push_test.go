package roleconnection

import (
	"context"
	"errors"
	"testing"
	"time"

	"rustlens/backend/internal/oauth/discord"
	"rustlens/backend/internal/store"
)

type fakeStore struct {
	row             *store.DiscordRoleConnection
	proGrantedAt    *time.Time
	displayName     string
	upsertCalled    int
	recordedHash    string
	recordedErrMsg  string
	revokeReason    string
	deleteCalled    int
	listActive      []store.DiscordRoleConnection
}

func (f *fakeStore) GetDiscordRoleConnection(_ context.Context, _ string) (*store.DiscordRoleConnection, error) {
	return f.row, nil
}
func (f *fakeStore) UpsertDiscordRoleConnection(_ context.Context, r store.DiscordRoleConnection) error {
	f.upsertCalled++
	f.row = &r
	return nil
}
func (f *fakeStore) DeleteDiscordRoleConnection(_ context.Context, _ string) error {
	f.deleteCalled++
	f.row = nil
	return nil
}
func (f *fakeStore) MarkDiscordRoleConnectionRevoked(_ context.Context, _, reason string) error {
	f.revokeReason = reason
	if f.row != nil {
		now := time.Now()
		f.row.RevokedAt = &now
	}
	return nil
}
func (f *fakeStore) RecordDiscordRoleConnectionPushed(_ context.Context, _, hash string) error {
	f.recordedHash = hash
	return nil
}
func (f *fakeStore) RecordDiscordRoleConnectionPushError(_ context.Context, _, msg string) error {
	f.recordedErrMsg = msg
	return nil
}
func (f *fakeStore) ListActiveDiscordRoleConnections(_ context.Context) ([]store.DiscordRoleConnection, error) {
	return f.listActive, nil
}
func (f *fakeStore) GetUserProGrantedAt(_ context.Context, _ string) (*time.Time, error) {
	return f.proGrantedAt, nil
}
func (f *fakeStore) GetAppUserDisplayName(_ context.Context, _ string) (string, error) {
	return f.displayName, nil
}

type fakeDiscord struct {
	putCalls           int
	putBody            discord.RoleConnectionBody
	putErr             error
	refreshCalls       int
	refreshAccess      string
	refreshRefresh     string
	refreshExpiresIn   int
	refreshErr         error
	receivedAccessTok  string
}

func (f *fakeDiscord) PutRoleConnection(_ context.Context, accessToken, _ string, body discord.RoleConnectionBody) error {
	f.putCalls++
	f.receivedAccessTok = accessToken
	f.putBody = body
	return f.putErr
}
func (f *fakeDiscord) RefreshToken(_ context.Context, _ string) (string, string, int, error) {
	f.refreshCalls++
	return f.refreshAccess, f.refreshRefresh, f.refreshExpiresIn, f.refreshErr
}

func TestSyncerPush_NoRow_NoOp(t *testing.T) {
	s := &fakeStore{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: &fakeDiscord{}, ApplicationID: "APP",
		Cipher: TokenCipher{Crypto: newTestCrypto(t)},
	})
	if err := syncer.Push(context.Background(), "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestSyncerPush_HappyPath_TokenStillValid(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, err := cipher.Encrypt("access-1")
	if err != nil {
		t.Fatalf("encrypt access: %v", err)
	}
	rt, rtNonce, err := cipher.Encrypt("refresh-1")
	if err != nil {
		t.Fatalf("encrypt refresh: %v", err)
	}
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID:            "uid-1",
			DiscordID:         "d1",
			AccessTokenCT:     at,
			AccessTokenNonce:  atNonce,
			RefreshTokenCT:    rt,
			RefreshTokenNonce: rtNonce,
			AccessExpiresAt:   now.Add(time.Hour),
			Scopes:            "identify role_connections.write",
		},
		proGrantedAt: &now,
		displayName:  "sem",
	}
	d := &fakeDiscord{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.refreshCalls != 0 {
		t.Errorf("refresh called %d times, want 0", d.refreshCalls)
	}
	if d.putCalls != 1 {
		t.Errorf("put called %d times, want 1", d.putCalls)
	}
	if d.receivedAccessTok != "access-1" {
		t.Errorf("access token = %q", d.receivedAccessTok)
	}
	if d.putBody.Metadata["is_pro"] != "1" {
		t.Errorf("is_pro = %q", d.putBody.Metadata["is_pro"])
	}
	if s.recordedHash == "" {
		t.Error("expected hash to be recorded")
	}
}

func TestSyncerPush_SkipsWhenHashUnchanged(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access-1")
	rt, rtNonce, _ := cipher.Encrypt("refresh-1")
	now := time.Now()
	tenMinAgo := now.Add(-10 * time.Minute)
	pre := BuildMetadata("sem", &now)
	preHash := HashMetadata(pre)
	row := store.DiscordRoleConnection{
		UserID: "uid-1", DiscordID: "d1",
		AccessTokenCT: at, AccessTokenNonce: atNonce,
		RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
		AccessExpiresAt: now.Add(time.Hour),
		Scopes:          "identify role_connections.write",
		LastPushedAt:    &tenMinAgo,
		LastPushedHash:  &preHash,
	}
	s := &fakeStore{row: &row, proGrantedAt: &now, displayName: "sem"}
	d := &fakeDiscord{}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.putCalls != 0 {
		t.Errorf("expected push skip, got %d puts", d.putCalls)
	}
}

func TestSyncerPush_RefreshesExpiredToken(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access-old")
	rt, rtNonce, _ := cipher.Encrypt("refresh-old")
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "uid-1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: now.Add(-time.Minute), // expired
			Scopes:          "identify role_connections.write",
		},
		proGrantedAt: &now, displayName: "sem",
	}
	d := &fakeDiscord{
		refreshAccess:    "access-new",
		refreshRefresh:   "refresh-new",
		refreshExpiresIn: 604800,
	}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	if err := syncer.Push(ctx, "uid-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if d.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", d.refreshCalls)
	}
	if d.receivedAccessTok != "access-new" {
		t.Errorf("put used %q, want access-new", d.receivedAccessTok)
	}
	if s.upsertCalled != 1 {
		t.Errorf("expected token row upsert after refresh, got %d", s.upsertCalled)
	}
}

func TestSyncerPush_RefreshRevoked_MarksRow(t *testing.T) {
	ctx := context.Background()
	cipher := TokenCipher{Crypto: newTestCrypto(t)}
	at, atNonce, _ := cipher.Encrypt("access")
	rt, rtNonce, _ := cipher.Encrypt("refresh")
	now := time.Now()
	s := &fakeStore{
		row: &store.DiscordRoleConnection{
			UserID: "uid-1", DiscordID: "d1",
			AccessTokenCT: at, AccessTokenNonce: atNonce,
			RefreshTokenCT: rt, RefreshTokenNonce: rtNonce,
			AccessExpiresAt: now.Add(-time.Minute),
			Scopes:          "identify role_connections.write",
		},
		proGrantedAt: &now, displayName: "sem",
	}
	d := &fakeDiscord{refreshErr: discord.ErrRefreshRevoked}
	syncer := NewSyncer(SyncerConfig{
		Store: s, Discord: d, ApplicationID: "APP", Cipher: cipher,
	})
	err := syncer.Push(ctx, "uid-1")
	if !errors.Is(err, discord.ErrRefreshRevoked) {
		t.Fatalf("err = %v, want ErrRefreshRevoked", err)
	}
	if s.revokeReason != "invalid_grant" {
		t.Errorf("revokeReason = %q", s.revokeReason)
	}
}
