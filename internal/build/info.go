package build

import (
	"runtime/debug"
	"strings"
)

var (
	// Version is populated via ldflags during release builds.
	// Falls back to runtime/debug for `go install` builds.
	Version = version()
	// Commit captures the source revision.
	Commit = commit()
	// Date contains the build timestamp.
	Date = date()
)

// version returns the version string, using ldflags if set,
// otherwise falling back to module version from go install.
func version() string {
	// If set via ldflags (GoReleaser), use that
	if v := versionFromLdflags; v != "" && v != "dev" {
		return v
	}

	// Otherwise try to get from runtime/debug (go install)
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			// Remove 'v' prefix if present for consistency
			return strings.TrimPrefix(info.Main.Version, "v")
		}
	}

	return "dev"
}

// commit returns the commit hash from build info if available.
func commit() string {
	if c := commitFromLdflags; c != "" {
		return c
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				// Return short hash (first 7 chars) for consistency
				if len(setting.Value) >= 7 {
					return setting.Value[:7]
				}
				return setting.Value
			}
		}
	}

	return ""
}

// date returns the build timestamp.
func date() string {
	if d := dateFromLdflags; d != "" {
		return d
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				return setting.Value
			}
		}
	}

	return ""
}

// These variables are set via ldflags during GoReleaser builds
var (
	versionFromLdflags = "dev"
	commitFromLdflags  = ""
	dateFromLdflags    = ""
)
