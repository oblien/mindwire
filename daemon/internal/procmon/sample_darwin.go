//go:build darwin

package procmon

import (
	"os/exec"
	"strconv"
	"strings"
)

// sampleProcs shells out to BSD `ps` — macOS has no /proc. This is a DEV-ONLY path (production
// runtimes are Linux); it keeps the console's resource monitor working on a developer's Mac. The
// columns `-o pgid=,rss=,time=` print headerless: process-group id, resident set size in KiB, and
// cumulative CPU time as [[dd-]hh:]mm:ss. Rows are aggregated per group; a parse failure skips a row.
func sampleProcs(pgids map[int]struct{}) map[int]procStat {
	out := make(map[int]procStat, len(pgids))
	data, err := exec.Command("ps", "-axo", "pgid=,rss=,time=").Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pgid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		if _, want := pgids[pgid]; !want {
			continue
		}
		rssKiB, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		s := out[pgid]
		s.RSSBytes += rssKiB * 1024
		s.CPUSeconds += parsePsTime(f[2])
		out[pgid] = s
	}
	return out
}

// parsePsTime parses BSD ps `time` ([[dd-]hh:]mm:ss) into seconds. Best-effort: unparseable segments
// count as zero rather than failing the whole sample.
func parsePsTime(s string) float64 {
	days := 0.0
	if i := strings.IndexByte(s, '-'); i >= 0 {
		days, _ = strconv.ParseFloat(s[:i], 64)
		s = s[i+1:]
	}
	secs := 0.0
	for _, p := range strings.Split(s, ":") {
		v, _ := strconv.ParseFloat(p, 64)
		secs = secs*60 + v
	}
	return days*86400 + secs
}
