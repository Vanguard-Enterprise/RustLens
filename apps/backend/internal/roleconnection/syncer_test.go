package roleconnection

import (
	"testing"
	"time"
)

func TestBuildMetadata_NotPro(t *testing.T) {
	body := BuildMetadata("displayname", nil)
	if body.Metadata["is_pro"] != "0" {
		t.Errorf("is_pro = %q, want 0", body.Metadata["is_pro"])
	}
	if _, hasSince := body.Metadata["pro_since"]; hasSince {
		t.Error("pro_since should be absent when not pro")
	}
	if body.PlatformName != "RustLens" || body.PlatformUsername != "displayname" {
		t.Errorf("platform fields = %+v", body)
	}
}

func TestBuildMetadata_Pro(t *testing.T) {
	at := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	body := BuildMetadata("sem", &at)
	if body.Metadata["is_pro"] != "1" {
		t.Errorf("is_pro = %q, want 1", body.Metadata["is_pro"])
	}
	if body.Metadata["pro_since"] != "2026-05-23T12:00:00+00:00" {
		t.Errorf("pro_since = %q", body.Metadata["pro_since"])
	}
}

func TestHashMetadata_Stable(t *testing.T) {
	at := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	a := HashMetadata(BuildMetadata("x", &at))
	b := HashMetadata(BuildMetadata("x", &at))
	if a != b {
		t.Errorf("hash unstable: %s != %s", a, b)
	}
	c := HashMetadata(BuildMetadata("x", nil))
	if a == c {
		t.Error("hash collision pro vs not-pro")
	}
}
