package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	cases := map[string]struct {
		info debug.BuildInfo
		want string
	}{
		"released binary": {
			info: debug.BuildInfo{Main: debug.Module{Path: ModulePath, Version: "v0.3.0"}},
			want: "v0.3.0",
		},
		"checkout build with a pseudo-version": {
			info: debug.BuildInfo{Main: debug.Module{Path: ModulePath, Version: "v0.3.1-0.20261006171036-614e1bb0a1b2+dirty"}},
			want: "v0.3.1-0.20261006171036-614e1bb0a1b2+dirty",
		},
		"checkout build without a version": {
			info: debug.BuildInfo{
				Main:     debug.Module{Path: ModulePath, Version: "(devel)"},
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "614e1bb0a1b2c3d4"}, {Key: "vcs.modified", Value: "true"}},
			},
			want: "dev+614e1bb.dirty",
		},
		"no version and no commit": {
			info: debug.BuildInfo{Main: debug.Module{Path: ModulePath, Version: "(devel)"}},
			want: Dev,
		},
		"used as a library": {
			info: debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "v1.0.0"},
				Deps: []*debug.Module{{Path: "golang.org/x/text", Version: "v0.20.0"}, {Path: ModulePath, Version: "v0.3.0"}},
			},
			want: "v0.3.0",
		},
		"library replaced by a local copy": {
			info: debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app"},
				Deps: []*debug.Module{{Path: ModulePath, Version: "v0.3.0", Replace: &debug.Module{Path: "../speccritic"}}},
			},
			want: Dev,
		},
		"library not in the build": {
			info: debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}},
			want: Dev,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := fromBuildInfo(&tc.info); got != tc.want {
				t.Errorf("fromBuildInfo = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePrefersABuildTimeOverride(t *testing.T) {
	if got := Resolve("v9.9.9"); got != "v9.9.9" {
		t.Errorf("Resolve(v9.9.9) = %q", got)
	}
	if got := Resolve(Dev); got != Version() {
		t.Errorf("Resolve(dev) = %q, want Version() %q", got, Version())
	}
	if Version() == "" {
		t.Error("Version() is empty")
	}
}
