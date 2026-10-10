package version

import "runtime/debug"

// Version is overridden at build time via -ldflags "-X github.com/sobakintech/maitake/internal/version.Version=...".
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	// Local builds without ldflags: identify the commit Go stamped into the binary.
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) >= 7 {
		Version = "dev-" + rev[:7]
		if dirty {
			Version += "-dirty"
		}
	}
}
