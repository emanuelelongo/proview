// Package buildinfo exposes metadata embedded by the build tooling.
package buildinfo

import (
	"runtime/debug"
	"time"
)

// These strings are populated with -ldflags -X by packaging/build.py.
var (
	Name       = "Proview"
	Version    = "DEV"
	Repository = "https://github.com/emanuelelongo/proview"
	Commit     string
	Date       string
)

type Info struct {
	Name, Version, Repository, Commit, Date string
}

func Current() Info {
	info := Info{Name, Version, Repository, Commit, Date}
	vcs, _ := debug.ReadBuildInfo()
	return withVCS(info, vcs)
}

func withVCS(info Info, vcs *debug.BuildInfo) Info {
	if vcs != nil {
		for _, setting := range vcs.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = setting.Value
				}
			}
		}
	}
	if info.Commit == "" {
		info.Commit = "Unknown"
	}
	if date, err := time.Parse(time.RFC3339, info.Date); err == nil {
		info.Date = date.UTC().Format(time.RFC3339)
	} else {
		info.Date = "Unknown"
	}
	return info
}
