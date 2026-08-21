//go:build linux

package procmon

import (
	"os"
	"strconv"
	"strings"
)

// clkTck is the kernel's USER_HZ — the jiffies-per-second unit of utime/stime in /proc/<pid>/stat.
// It is effectively always 100 on Linux (x86/arm), a build-time kernel constant; we hardcode it
// rather than shelling out to `getconf CLK_TCK` so this stays dependency-free and behaves identically
// on Alpine/BusyBox and Debian/coreutils.
const clkTck = 100.0

// sampleProcs reads /proc directly — the kernel interface, IDENTICAL across every Linux userland
// (Alpine/BusyBox, Debian/coreutils, …) because it doesn't depend on `ps` flag support at all. It
// walks every process and, for those whose process-group id is in pgids, accumulates cumulative CPU
// seconds (utime+stime) and RSS bytes into that group. A process that exits mid-scan is skipped (a
// normal race), and a group with no live process simply doesn't appear in the result.
func sampleProcs(pgids map[int]struct{}) map[int]procStat {
	out := make(map[int]procStat, len(pgids))
	pageSize := uint64(os.Getpagesize())

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if name[0] < '0' || name[0] > '9' { // only numeric pid directories
			continue
		}
		data, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue // process exited between ReadDir and ReadFile
		}
		pgrp, cpu, rss, ok := parseStat(string(data), pageSize)
		if !ok {
			continue
		}
		if _, want := pgids[pgrp]; !want {
			continue
		}
		s := out[pgrp]
		s.CPUSeconds += cpu
		s.RSSBytes += rss
		out[pgrp] = s
	}
	return out
}

// parseStat extracts pgrp (field 5), utime+stime (fields 14+15, jiffies → seconds) and rss (field 24,
// pages → bytes) from a /proc/<pid>/stat line. The comm field (2) is parenthesised and may itself
// contain spaces and ')', so we split AFTER the LAST ')': everything past it is plain space-separated
// and positionally stable. In that remainder (0-based) field 3 (state) is index 0, so field N is
// index N-3: pgrp→2, utime→11, stime→12, rss→21.
func parseStat(line string, pageSize uint64) (pgrp int, cpuSeconds float64, rssBytes uint64, ok bool) {
	rp := strings.LastIndexByte(line, ')')
	if rp < 0 {
		return 0, 0, 0, false
	}
	rest := strings.Fields(line[rp+1:])
	if len(rest) < 22 {
		return 0, 0, 0, false
	}
	pgrp, err := strconv.Atoi(rest[2])
	if err != nil {
		return 0, 0, 0, false
	}
	utime, err1 := strconv.ParseUint(rest[11], 10, 64)
	stime, err2 := strconv.ParseUint(rest[12], 10, 64)
	rssPages, err3 := strconv.ParseInt(rest[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	if rssPages < 0 {
		rssPages = 0
	}
	cpuSeconds = float64(utime+stime) / clkTck
	rssBytes = uint64(rssPages) * pageSize
	return pgrp, cpuSeconds, rssBytes, true
}
