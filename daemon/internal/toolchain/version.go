package toolchain

import (
	"regexp"
	"strconv"
	"strings"
)

// Catalog versions are exact SemVer values, never npm tags, ranges or shell text.
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)
var outputVersion = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?\b`)

func validVersion(v string) bool {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	for _, n := range m[1:4] {
		if _, err := strconv.ParseUint(n, 10, 64); err != nil {
			return false
		}
	}
	if m[4] != "" {
		for _, id := range strings.Split(m[4], ".") {
			if id == "" {
				return false
			}
			if _, err := strconv.ParseUint(id, 10, 64); err == nil && len(id) > 1 && id[0] == '0' {
				return false
			}
		}
	}
	return true
}

// ParseVersion understands both plain versions and native banners ("codex-cli 0.155.0").
func ParseVersion(output string) string {
	v := outputVersion.FindString(output)
	if validVersion(v) {
		return v
	}
	return ""
}

func compare(a, b string) int {
	am, bm := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	if am == nil || bm == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i <= 3; i++ {
		av, _ := strconv.ParseUint(am[i], 10, 64)
		bv, _ := strconv.ParseUint(bm[i], 10, 64)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	if am[4] == bm[4] {
		return 0
	}
	if am[4] == "" {
		return 1
	}
	if bm[4] == "" {
		return -1
	}
	ap, bp := strings.Split(am[4], "."), strings.Split(bm[4], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		av, ae := strconv.ParseUint(ap[i], 10, 64)
		bv, be := strconv.ParseUint(bp[i], 10, 64)
		if ae == nil && be == nil {
			if av < bv {
				return -1
			}
			return 1
		}
		if ae == nil {
			return -1
		}
		if be == nil {
			return 1
		}
		return strings.Compare(ap[i], bp[i])
	}
	if len(ap) < len(bp) {
		return -1
	}
	return 1
}
