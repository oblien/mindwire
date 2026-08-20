//go:build linux

package procmon

import "testing"

// TestParseStat covers the delicate part of the Linux sampler: the comm field (2) is parenthesised and
// may itself contain spaces and ')', so parsing MUST split after the LAST ')'. A naive Fields() split
// would mis-index every field for such a process.
func TestParseStat(t *testing.T) {
	const pageSize = 4096
	// comm = "weird )( name" — embedded spaces AND parens, the adversarial case. After the last ')':
	// state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime ... rss
	line := "4242 (weird )( name) R 1 4242 4242 0 -1 0 0 0 0 0 100 50 0 0 20 0 1 0 0 0 10\n"

	pgrp, cpu, rss, ok := parseStat(line, pageSize)
	if !ok {
		t.Fatal("parseStat failed on a valid line")
	}
	if pgrp != 4242 {
		t.Fatalf("pgrp: got %d want 4242", pgrp)
	}
	if cpu != 1.5 { // (utime 100 + stime 50) / clkTck 100
		t.Fatalf("cpuSeconds: got %v want 1.5", cpu)
	}
	if rss != 10*pageSize {
		t.Fatalf("rssBytes: got %d want %d", rss, 10*pageSize)
	}
}

// TestParseStatRejects covers the malformed cases: no ')' at all, and too few fields after it.
func TestParseStatRejects(t *testing.T) {
	if _, _, _, ok := parseStat("4242 no-paren R 1 2\n", 4096); ok {
		t.Fatal("a line with no ')' must be rejected")
	}
	if _, _, _, ok := parseStat("4242 (comm) R 1 2\n", 4096); ok {
		t.Fatal("a line with too few fields must be rejected")
	}
}
