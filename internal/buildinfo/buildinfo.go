// Package buildinfo reports honest build provenance. Values are stamped with
// -ldflags at build time; when they are not stamped the package says so
// instead of inventing a version (FR-042).
package buildinfo

// These are set by the linker (see the Makefile). The zero values mean "not stamped".
var (
	version = ""
	commit  = ""
	dirty   = ""
)

// Info is the build provenance exposed on /healthz and by `jsctl version`.
type Info struct {
	Version    string `json:"version"`
	Commit     string `json:"source_commit"`
	Dirty      bool   `json:"source_dirty"`
	Stamped    bool   `json:"stamped"`
	DirtyKnown bool   `json:"source_dirty_known"`
}

// Get returns the build information. An unstamped binary reports
// Version "unstamped" and Commit "unknown" with Stamped=false.
func Get() Info { return from(version, commit, dirty) }

func from(v, c, d string) Info {
	if v == "" || c == "" {
		return Info{Version: "unstamped", Commit: "unknown", Stamped: false}
	}
	i := Info{Version: v, Commit: c, Stamped: true}
	switch d {
	case "true":
		i.Dirty, i.DirtyKnown = true, true
	case "false":
		i.DirtyKnown = true
	}
	return i
}
