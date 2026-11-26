// Package version provides semantic version comparison, GitHub release update
// checks, and structured rendering for gsvc version output.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

type semVer struct {
	major int
	minor int
	patch int
	pre   []string
}

func parseSemVer(raw string) (semVer, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return semVer{}, fmt.Errorf("invalid semver %q", raw)
	}

	// Strip build metadata (+...)
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}

	var pre []string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		preStr := s[idx+1:]
		if preStr == "" {
			return semVer{}, fmt.Errorf("invalid semver %q: empty prerelease", raw)
		}
		pre = strings.Split(preStr, ".")
		for _, p := range pre {
			if p == "" {
				return semVer{}, fmt.Errorf("invalid semver %q: empty prerelease identifier", raw)
			}
		}
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semVer{}, fmt.Errorf("invalid semver %q", raw)
	}

	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" || !allDigits(p) {
			return semVer{}, fmt.Errorf("invalid semver %q", raw)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return semVer{}, fmt.Errorf("invalid semver %q: %w", raw, err)
		}
		nums[i] = n
	}

	return semVer{
		major: nums[0],
		minor: nums[1],
		patch: nums[2],
		pre:   pre,
	}, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// Compare compares two SemVer strings according to SemVer 2.0.0 precedence rules.
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
func Compare(a, b string) (int, error) {
	va, err := parseSemVer(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseSemVer(b)
	if err != nil {
		return 0, err
	}

	if c := cmpInt(va.major, vb.major); c != 0 {
		return c, nil
	}
	if c := cmpInt(va.minor, vb.minor); c != 0 {
		return c, nil
	}
	if c := cmpInt(va.patch, vb.patch); c != 0 {
		return c, nil
	}

	// Absence of prerelease has higher precedence than any prerelease.
	if len(va.pre) == 0 && len(vb.pre) == 0 {
		return 0, nil
	}
	if len(va.pre) == 0 {
		return 1, nil
	}
	if len(vb.pre) == 0 {
		return -1, nil
	}

	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		if va.pre[i] == vb.pre[i] {
			continue
		}
		aNum := allDigits(va.pre[i])
		bNum := allDigits(vb.pre[i])
		switch {
		case aNum && bNum:
			na, errA := strconv.Atoi(va.pre[i])
			nb, errB := strconv.Atoi(vb.pre[i])
			if errA == nil && errB == nil {
				if c := cmpInt(na, nb); c != 0 {
					return c, nil
				}
			}
		case aNum != bNum:
			if aNum {
				return -1, nil
			}
			return 1, nil
		default:
			if va.pre[i] < vb.pre[i] {
				return -1, nil
			}
			return 1, nil
		}
	}

	return cmpInt(len(va.pre), len(vb.pre)), nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// IsDevBuild reports whether v represents a development, snapshot, or
// non-semver build that should not be compared as a release version.
func IsDevBuild(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" || s == "dev" || s == "none" {
		return true
	}
	if strings.HasSuffix(s, "-dirty") || strings.HasSuffix(s, "-next") {
		return true
	}
	_, err := parseSemVer(s)
	return err != nil
}
