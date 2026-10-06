// Package buildinfo reports which version of SpecCritic is running.
//
// The Go toolchain records the module version in every binary: the release
// tag for `go install github.com/dshills/speccritic/cmd/speccritic@v0.3.0`,
// and a pseudo-version derived from the commit for a build in a git
// checkout. Reading it at run time means a report names its version without
// anyone passing -ldflags at build time.
package buildinfo

import (
	"runtime/debug"
	"sync"
)

// ModulePath is the path of the SpecCritic module.
const ModulePath = "github.com/dshills/speccritic"

// Dev is reported when no version can be determined.
const Dev = "dev"

var (
	once   sync.Once
	cached string
)

// Version returns the running SpecCritic version. It is the version Go
// recorded for the SpecCritic module, whether that module is the main
// module (the speccritic binaries) or a dependency (a program using the
// library); failing that, the commit the binary was built from; failing
// that, Dev.
func Version() string {
	once.Do(func() {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			cached = Dev
			return
		}
		cached = fromBuildInfo(info)
	})
	return cached
}

// Resolve returns override when it was set at build time (anything but
// empty or Dev), and Version otherwise.
func Resolve(override string) string {
	if override != "" && override != Dev {
		return override
	}
	return Version()
}

func fromBuildInfo(info *debug.BuildInfo) string {
	if info.Main.Path == ModulePath {
		if v := usable(info.Main.Version); v != "" {
			return v
		}
		return fromVCS(info.Settings)
	}
	for _, dep := range info.Deps {
		if dep.Path != ModulePath {
			continue
		}
		if dep.Replace != nil {
			if v := usable(dep.Replace.Version); v != "" {
				return v
			}
			return Dev
		}
		if v := usable(dep.Version); v != "" {
			return v
		}
	}
	return Dev
}

// usable returns v unless it is empty or the "(devel)" placeholder Go uses
// when it has no version to record.
func usable(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	return v
}

// fromVCS builds a version from the commit recorded at build time, such as
// "dev+614e1bb" or "dev+614e1bb.dirty".
func fromVCS(settings []debug.BuildSetting) string {
	var revision string
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return Dev
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	v := Dev + "+" + revision
	if modified {
		v += ".dirty"
	}
	return v
}
