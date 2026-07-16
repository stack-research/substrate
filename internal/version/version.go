package version

import (
	"runtime/debug"
	"strings"
)

// Version is a variable so release builds can inject a tag with -ldflags -X.
var Version = "0.2.0-dev"

const Runtime = "go"

// Full returns a build-distinguishing version. Go embeds the source revision
// and dirty state in normal builds, so local installs from different worktree
// states no longer all present as the same ambiguous development version.
func Full() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	var revision string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return format(Version, revision, modified)
}

func format(base, revision string, modified bool) string {
	if revision == "" {
		return base
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	label := base + "+" + strings.ToLower(revision)
	if modified {
		label += ".dirty"
	}
	return label
}
