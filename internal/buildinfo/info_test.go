package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestEmbeddedMetadataTakesPrecedenceOverVCS(t *testing.T) {
	info := Info{"Proview", "v1-dev.42", "https://github.com/example/proview", "embedded-sha", "2026-10-07T12:20:30+02:00"}
	got := withVCS(info, &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "vcs-sha"},
		{Key: "vcs.time", Value: "2025-01-01T00:00:00Z"},
	}})
	info.Date = "2026-10-07T10:20:30Z"
	if got != info {
		t.Fatalf("got %+v, want %+v", got, info)
	}
}

func TestDirectGoBuildUsesVCSMetadata(t *testing.T) {
	info := Info{Name: "Proview", Version: "DEV", Repository: "https://github.com/emanuelelongo/proview"}
	got := withVCS(info, &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "vcs-sha"},
		{Key: "vcs.time", Value: "2026-10-07T10:20:30Z"},
	}})
	if got.Commit != "vcs-sha" || got.Date != "2026-10-07T10:20:30Z" || got.Version != "DEV" {
		t.Fatalf("unexpected local metadata: %+v", got)
	}
}

func TestMissingOrInvalidMetadataUsesUnknown(t *testing.T) {
	for _, date := range []string{"", "Unknown", "invalid"} {
		got := withVCS(Info{Version: "DEV", Date: date}, nil)
		if got.Commit != "Unknown" || got.Date != "Unknown" {
			t.Fatalf("unexpected fallback metadata: %+v", got)
		}
	}
}
