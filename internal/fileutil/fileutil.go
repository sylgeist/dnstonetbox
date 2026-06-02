// Package fileutil provides shared helpers for the config generators:
// idempotent atomic file writes and a unified-diff renderer for --dry-run.
package fileutil

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteIfChanged writes data to path only when it differs from the current
// contents, returning whether a write occurred. The write is atomic: data is
// written to a temporary file in the same directory and renamed into place, so
// a reader (NSD/Unbound) never observes a partially written file and a crash
// mid-write cannot corrupt an existing file.
func WriteIfChanged(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we don't make it to the rename.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

// UnifiedDiff returns a unified diff string (diff -u format) between old and new.
// label is used as the filename in the header. Returns "" if content is identical.
func UnifiedDiff(label string, old, new []byte) string {
	const ctx = 3

	splitLines := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}

	oldLines := splitLines(old)
	newLines := splitLines(new)
	edits := diffLines(oldLines, newLines)
	n := len(edits)

	var changed []int
	for i, e := range edits {
		if e.kind != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return ""
	}

	type hunkRange struct{ lo, hi int }
	var ranges []hunkRange
	lo := max(0, changed[0]-ctx)
	hi := min(n, changed[0]+1+ctx)
	for _, idx := range changed[1:] {
		if max(0, idx-ctx) <= hi {
			hi = min(n, idx+1+ctx)
		} else {
			ranges = append(ranges, hunkRange{lo, hi})
			lo = max(0, idx-ctx)
			hi = min(n, idx+1+ctx)
		}
	}
	ranges = append(ranges, hunkRange{lo, hi})

	oldNums := make([]int, n)
	newNums := make([]int, n)
	ol, nl := 1, 1
	for i, e := range edits {
		oldNums[i] = ol
		newNums[i] = nl
		if e.kind != '+' {
			ol++
		}
		if e.kind != '-' {
			nl++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s (current)\n+++ %s (would be written)\n", label, label)
	for _, r := range ranges {
		oldCount, newCount := 0, 0
		for _, e := range edits[r.lo:r.hi] {
			if e.kind != '+' {
				oldCount++
			}
			if e.kind != '-' {
				newCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldNums[r.lo], oldCount, newNums[r.lo], newCount)
		for _, e := range edits[r.lo:r.hi] {
			fmt.Fprintf(&sb, "%c%s\n", e.kind, e.line)
		}
	}
	return sb.String()
}

type diffEdit struct {
	kind rune // ' ' equal, '-' delete, '+' insert
	line string
}

// diffLines computes a line-by-line edit script using LCS.
func diffLines(a, b []string) []diffEdit {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var edits []diffEdit
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1]:
			edits = append(edits, diffEdit{' ', a[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			edits = append(edits, diffEdit{'+', b[j-1]})
			j--
		default:
			edits = append(edits, diffEdit{'-', a[i-1]})
			i--
		}
	}
	for l, r := 0, len(edits)-1; l < r; l, r = l+1, r-1 {
		edits[l], edits[r] = edits[r], edits[l]
	}
	return edits
}
