package cli

import "runtime/debug"

// Revision is injected by reproducible package builds via -ldflags -X.
// Source-only packages use a content hash when no clean Git revision exists.
var Revision string

func buildRevision() string {
	if Revision != "" {
		return Revision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return vcsRevision(info.Settings)
}

func vcsRevision(settings []debug.BuildSetting) string {
	revision, modified := "", false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}
