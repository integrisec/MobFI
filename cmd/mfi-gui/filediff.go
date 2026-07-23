package main

import (
	"bytes"
	"io"
	"os"
	"strings"
)

const (
	maxDiffFileBytes = 4 << 20 // 4 MB per side
	maxDiffLines     = 3000    // LCS is O(n*m); cap to bound memory
)

// DiffRow is one aligned line pair for the side-by-side file diff.
type DiffRow struct {
	LeftNum  int    `json:"left_num"`  // 0 = no line on the left
	RightNum int    `json:"right_num"` // 0 = no line on the right
	Left     string `json:"left"`
	Right    string `json:"right"`
	Type     string `json:"type"` // same | add | del | change
}

// FileDiffResult is the side-by-side diff of two files.
type FileDiffResult struct {
	Rows         []DiffRow `json:"rows"`
	Binary       bool      `json:"binary"`
	TooLarge     bool      `json:"too_large"`
	LeftMissing  bool      `json:"left_missing"`
	RightMissing bool      `json:"right_missing"`
}

// FileDiff computes an aligned, line-level diff of two files for the
// side-by-side viewer. Binary or oversized files are flagged instead.
func (g *GUI) FileDiff(pathA, pathB string) (FileDiffResult, error) {
	var res FileDiffResult
	sa, ea := statSize(pathA)
	sb, eb := statSize(pathB)
	res.LeftMissing = !ea
	res.RightMissing = !eb
	if (ea && sa > maxDiffFileBytes) || (eb && sb > maxDiffFileBytes) {
		res.TooLarge = true
		return res, nil
	}
	a, abin := readLines(pathA)
	b, bbin := readLines(pathB)
	if abin || bbin {
		res.Binary = true
		return res, nil
	}
	if len(a) > maxDiffLines || len(b) > maxDiffLines {
		res.TooLarge = true
		return res, nil
	}
	res.Rows = lineDiff(a, b)
	return res, nil
}

func statSize(path string) (int64, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// readLines returns a file's lines, or binary=true if it looks binary (a NUL
// byte). A missing file yields no lines (treated as empty).
func readLines(path string) (lines []string, binary bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	buf := make([]byte, maxDiffFileBytes)
	n, _ := io.ReadFull(f, buf)
	data := buf[:n]
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, true
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines = strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // drop the empty element after a trailing newline
	}
	return lines, false
}

// lineDiff aligns two line slices via an LCS, pairing deletion/insertion
// runs into "change" rows where they overlap.
func lineDiff(a, b []string) []DiffRow {
	n, m := len(a), len(b)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var rows []DiffRow
	ln, rn := 0, 0
	flush := func(dels, adds []string) {
		k := 0
		for k < len(dels) && k < len(adds) {
			ln++
			rn++
			rows = append(rows, DiffRow{ln, rn, dels[k], adds[k], "change"})
			k++
		}
		for ; k < len(dels); k++ {
			ln++
			rows = append(rows, DiffRow{ln, 0, dels[k], "", "del"})
		}
		for ; k < len(adds); k++ {
			rn++
			rows = append(rows, DiffRow{0, rn, "", adds[k], "add"})
		}
	}

	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ln++
			rn++
			rows = append(rows, DiffRow{ln, rn, a[i], a[i], "same"})
			i++
			j++
			continue
		}
		var dels, adds []string
		for i < n && j < m && a[i] != b[j] {
			if dp[i+1][j] >= dp[i][j+1] {
				dels = append(dels, a[i])
				i++
			} else {
				adds = append(adds, b[j])
				j++
			}
		}
		flush(dels, adds)
	}
	if i < n || j < m {
		var dels, adds []string
		for ; i < n; i++ {
			dels = append(dels, a[i])
		}
		for ; j < m; j++ {
			adds = append(adds, b[j])
		}
		flush(dels, adds)
	}
	return rows
}
