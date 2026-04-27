package httpapi

import (
	"testing"
	"time"

	"rustlens/backend/internal/store"
)

func TestSteamProfileStatusFromStatesPrefersOnlineState(t *testing.T) {
	oldOnline := time.Unix(1767225600, 0).UTC()
	newOnline := time.Unix(1767312000, 0).UTC()
	recentOffline := time.Unix(1767398400, 0).UTC()

	status := steamProfileStatusFromStates([]store.PlayerState{
		{ServerBmID: "offline", ServerName: "Offline Server", IsOnline: false, OfflineSince: &recentOffline},
		{ServerBmID: "old-online", ServerName: "Old Online", IsOnline: true, OnlineSince: &oldOnline},
		{ServerBmID: "new-online", ServerName: "New Online", IsOnline: true, OnlineSince: &newOnline},
	})

	if status == nil {
		t.Fatal("expected status")
	}
	if !status.IsOnline || status.ServerBmID != "new-online" || status.OnlineUnix == nil || *status.OnlineUnix != newOnline.Unix() {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestSteamProfileStatusFromStatesUsesMostRecentOfflineState(t *testing.T) {
	oldOffline := time.Unix(1767225600, 0).UTC()
	newOffline := time.Unix(1767312000, 0).UTC()

	status := steamProfileStatusFromStates([]store.PlayerState{
		{ServerBmID: "old-offline", ServerName: "Old Offline", IsOnline: false, OfflineSince: &oldOffline},
		{ServerBmID: "new-offline", ServerName: "New Offline", IsOnline: false, OfflineSince: &newOffline},
	})

	if status == nil {
		t.Fatal("expected status")
	}
	if status.IsOnline || status.ServerBmID != "new-offline" || status.OfflineUnix == nil || *status.OfflineUnix != newOffline.Unix() {
		t.Fatalf("unexpected status: %#v", status)
	}
}
