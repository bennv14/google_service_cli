package version

import "testing"

func TestCompareSemVer(t *testing.T) {
	tests := []struct {
		name    string
		a       string
		b       string
		want    int
		wantErr bool
	}{
		{name: "equal with v prefix", a: "v3.0.1", b: "v3.0.1", want: 0},
		{name: "equal without v prefix", a: "3.0.1", b: "v3.0.1", want: 0},
		{name: "patch older", a: "v3.0.1", b: "v3.0.2", want: -1},
		{name: "patch newer", a: "v3.0.2", b: "v3.0.1", want: 1},
		{name: "minor multi-digit", a: "v2.9.0", b: "v2.10.0", want: -1},
		{name: "major newer", a: "v3.0.0", b: "v2.9.9", want: 1},
		{name: "prerelease less than release", a: "v3.0.1-rc.1", b: "v3.0.1", want: -1},
		{name: "release greater than prerelease", a: "v3.0.1", b: "v3.0.1-rc.1", want: 1},
		{name: "prerelease numeric comparison", a: "v3.0.1-rc.2", b: "v3.0.1-rc.10", want: -1},
		{name: "prerelease lexical comparison", a: "v3.0.1-alpha", b: "v3.0.1-beta", want: -1},
		{name: "prerelease numeric less than lexical", a: "v3.0.1-1", b: "v3.0.1-alpha", want: -1},
		{name: "prerelease shorter less than longer", a: "v3.0.1-rc", b: "v3.0.1-rc.1", want: -1},
		{name: "build metadata ignored", a: "v3.0.1+build.1", b: "v3.0.1+build.2", want: 0},
		{name: "invalid dev", a: "dev", b: "v3.0.1", wantErr: true},
		{name: "invalid empty", a: "", b: "v3.0.1", wantErr: true},
		{name: "invalid short", a: "v3.0", b: "v3.0.1", wantErr: true},
		{name: "invalid negative", a: "v-1.0.0", b: "v3.0.1", wantErr: true},
		{name: "invalid right side", a: "v3.0.1", b: "not-a-version", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compare(tc.a, tc.b)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Compare(%q, %q) error = nil, want error", tc.a, tc.b)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compare(%q, %q) unexpected error: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"dev", true},
		{"", true},
		{"v3.0.1-dirty", true},
		{"v3.0.1-3-g7e28430-dirty", true},
		{"v3.0.2-next", true},
		{"abc1234", true},
		{"v3.0.1", false},
		{"3.0.1", false},
		{"v3.1.0-rc.1", false},
	}

	for _, tc := range tests {
		if got := IsDevBuild(tc.version); got != tc.want {
			t.Errorf("IsDevBuild(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}
