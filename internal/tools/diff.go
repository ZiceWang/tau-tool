package tools

import (
	"fmt"
	"strings"
)

// A diffPart is a run of lines that are equal ('='), removed ('-'), or added
// ('+'). Mirrors the parts returned by the JS "diff" package's diffLines.
type diffPart struct {
	op    byte
	value string // lines joined by "\n"
}

// diffLines computes a line-level diff between two strings using Myers' O(ND)
// algorithm. Lines are compared without their trailing newline; a trailing
// newline on the last line does not create an extra empty line.
func diffLines(oldContent, newContent string) []diffPart {
	oldLines := splitLinesForCounting(oldContent)
	newLines := splitLinesForCounting(newContent)
	edits := myersEditScript(oldLines, newLines)
	if len(edits) == 0 {
		return nil
	}

	var parts []diffPart
	var cur *diffPart
	flush := func() {
		if cur != nil {
			parts = append(parts, *cur)
			cur = nil
		}
	}
	for _, e := range edits {
		if cur != nil && cur.op == e.op {
			if cur.value == "" {
				cur.value = e.line
			} else {
				cur.value += "\n" + e.line
			}
			continue
		}
		flush()
		cur = &diffPart{op: e.op, value: e.line}
	}
	flush()
	return parts
}

type editOp struct {
	op   byte
	line string
}

func myersEditScript(a, b []string) []editOp {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}

	offset := max
	makeV := func() []int {
		v := make([]int, 2*max+1)
		for i := range v {
			v[i] = -1
		}
		return v
	}

	// trace[i] holds the v array after processing depth i.
	v := makeV()
	// Depth 0: follow the common prefix from (0,0).
	d0 := 0
	for d0 < n && d0 < m && a[d0] == b[d0] {
		d0++
	}
	v[offset] = d0
	trace := [][]int{v}

	// Identical inputs: the depth-0 snake covers everything.
	if d0 == n && d0 == m {
		edits := make([]editOp, n)
		for i := 0; i < n; i++ {
			edits[i] = editOp{'=', a[i]}
		}
		return edits
	}

	found := false
	var d int
	for d = 1; d <= max && !found; d++ {
		prev := trace[d-1]
		cur := make([]int, len(prev))
		copy(cur, prev)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && prev[offset+k-1] < prev[offset+k+1]) {
				x = prev[offset+k+1]
			} else {
				x = prev[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			cur[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
		trace = append(trace, cur)
	}

	// Backtrack to build the edit script (in reverse).
	var edits []editOp
	x, y := n, m
	for d = len(trace) - 1; d >= 1; d-- {
		vPrev := trace[d-1]
		k := x - y
		var prevK int
		if k == -d || (k != d && vPrev[offset+k-1] < vPrev[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vPrev[offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			edits = append(edits, editOp{'=', a[x-1]})
			x--
			y--
		}
		if x == prevX {
			edits = append(edits, editOp{'+', b[y-1]})
			y--
		} else {
			edits = append(edits, editOp{'-', a[x-1]})
			x--
		}
	}

	// Remaining common prefix from the d=0 diagonal.
	for x > 0 && y > 0 {
		edits = append(edits, editOp{'=', a[x-1]})
		x--
		y--
	}

	// Reverse into forward order.
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}
	return edits
}

// padStart left-pads s with spaces to the given width (JS padStart parity).
func padStart(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// generateDiffString produces the display-oriented diff used by pi's edit tool
// renderer: line numbers, +/- markers, and context lines with "..." skips.
func generateDiffString(oldContent, newContent string, contextLines int) (string, int) {
	if contextLines == 0 {
		contextLines = 4
	}
	parts := diffLines(oldContent, newContent)

	var out []string
	oldLines := splitLinesForCounting(oldContent)
	newLines := splitLinesForCounting(newContent)
	maxLineNum := len(oldLines)
	if len(newLines) > maxLineNum {
		maxLineNum = len(newLines)
	}
	lineNumWidth := len(itoa(maxLineNum))

	oldLineNum, newLineNum := 1, 1
	lastWasChange := false
	firstChangedLine := 0

	for i, part := range parts {
		raw := strings.Split(part.value, "\n")
		if len(raw) > 0 && raw[len(raw)-1] == "" {
			raw = raw[:len(raw)-1]
		}

		if part.op == '+' || part.op == '-' {
			if firstChangedLine == 0 {
				firstChangedLine = newLineNum
			}
			for _, line := range raw {
				if part.op == '+' {
					out = append(out, "+"+padStart(itoa(newLineNum), lineNumWidth)+" "+line)
					newLineNum++
				} else {
					out = append(out, "-"+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
					oldLineNum++
				}
			}
			lastWasChange = true
			continue
		}

		// Context lines.
		nextPartIsChange := i < len(parts)-1 && (parts[i+1].op == '+' || parts[i+1].op == '-')
		hasLeadingChange := lastWasChange
		hasTrailingChange := nextPartIsChange
		lastWasChange = false

		switch {
		case hasLeadingChange && hasTrailingChange:
			if len(raw) <= contextLines*2 {
				for _, line := range raw {
					out = append(out, " "+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
					oldLineNum++
					newLineNum++
				}
			} else {
				leading := raw[:contextLines]
				trailing := raw[len(raw)-contextLines:]
				skipped := len(raw) - len(leading) - len(trailing)
				for _, line := range leading {
					out = append(out, " "+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
					oldLineNum++
					newLineNum++
				}
				out = append(out, " "+padStart("", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
				for _, line := range trailing {
					out = append(out, " "+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
					oldLineNum++
					newLineNum++
				}
			}
		case hasLeadingChange:
			shown := raw
			if len(shown) > contextLines {
				shown = raw[:contextLines]
			}
			skipped := len(raw) - len(shown)
			for _, line := range shown {
				out = append(out, " "+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
				oldLineNum++
				newLineNum++
			}
			if skipped > 0 {
				out = append(out, " "+padStart("", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
			}
		case hasTrailingChange:
			skipped := 0
			if len(raw) > contextLines {
				skipped = len(raw) - contextLines
			}
			if skipped > 0 {
				out = append(out, " "+padStart("", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
			}
			for _, line := range raw[skipped:] {
				out = append(out, " "+padStart(itoa(oldLineNum), lineNumWidth)+" "+line)
				oldLineNum++
				newLineNum++
			}
		default:
			oldLineNum += len(raw)
			newLineNum += len(raw)
		}
	}

	return strings.Join(out, "\n"), firstChangedLine
}

// generateUnifiedPatch produces a standard unified patch with the given number
// of context lines, headers only (no timestamps), mirroring pi's edit tool.
func generateUnifiedPatch(path, oldContent, newContent string, contextLines int) string {
	if contextLines == 0 {
		contextLines = 4
	}
	parts := diffLines(oldContent, newContent)

	hasChange := false
	for _, p := range parts {
		if p.op != '=' {
			hasChange = true
			break
		}
	}
	if !hasChange {
		return ""
	}

	type hunk struct {
		oldStart, oldCount, newStart, newCount int
		lines                                  []string
	}

	// Precompute running old/new indices for each part.
	type indexedPart struct {
		part     diffPart
		oldStart int
		newStart int
	}
	indexed := make([]indexedPart, len(parts))
	oi, ni := 0, 0
	for idx, p := range parts {
		indexed[idx] = indexedPart{part: p, oldStart: oi, newStart: ni}
		n := 1 + strings.Count(p.value, "\n")
		switch p.op {
		case '=':
			oi += n
			ni += n
		case '-':
			oi += n
		case '+':
			ni += n
		}
	}

	// Group consecutive non-equal parts; merge groups separated by a gap of
	// equal lines <= 2*contextLines.
	type group struct {
		first, last int // indices into indexed
	}
	var groups []group
	for i := 0; i < len(indexed); i++ {
		if indexed[i].part.op == '=' {
			continue
		}
		g := group{first: i, last: i}
		for i+1 < len(indexed) && indexed[i+1].part.op != '=' {
			i++
			g.last = i
		}
		groups = append(groups, g)
	}

	oldLines := splitLinesForCounting(oldContent)
	newLines := splitLinesForCounting(newContent)

	var hunks []hunk
	for gi := 0; gi < len(groups); gi++ {
		g := groups[gi]
		// Merge with the next group if the equal gap is small enough.
		for gi+1 < len(groups) {
			next := groups[gi+1]
			gapPart := indexed[g.last+1] // the equal part separating the groups
			gapLines := 1 + strings.Count(gapPart.part.value, "\n")
			if gapLines > 2*contextLines {
				break
			}
			g.last = next.last
			gi++
		}

		first := indexed[g.first]
		last := indexed[g.last]
		h := hunk{
			oldStart: maxInt(0, first.oldStart-contextLines),
			newStart: maxInt(0, first.newStart-contextLines),
		}
		// last old/new index after the final changed part.
		lastOldEnd := last.oldStart
		lastNewEnd := last.newStart
		n := 1 + strings.Count(last.part.value, "\n")
		if last.part.op == '-' || last.part.op == '=' {
			lastOldEnd += n
		}
		if last.part.op == '+' || last.part.op == '=' {
			lastNewEnd += n
		}
		h.oldCount = minInt(len(oldLines), lastOldEnd+contextLines) - h.oldStart
		h.newCount = minInt(len(newLines), lastNewEnd+contextLines) - h.newStart

		hunkOldEnd := h.oldStart + h.oldCount
		hunkNewEnd := h.newStart + h.newCount

		// Gather lines in order: leading context, changes (and internal equal
		// parts for merged groups), trailing context. Range checks clip the
		// context parts to the hunk's old/new boundaries.
		start := g.first
		if g.first > 0 {
			start = g.first - 1
		}
		end := g.last
		if g.last+1 < len(indexed) {
			end = g.last + 1
		}
		for pidx := start; pidx <= end; pidx++ {
			ip := indexed[pidx]
			ls := strings.Split(ip.part.value, "\n")
			for k, l := range ls {
				var in bool
				switch ip.part.op {
				case '-':
					in = ip.oldStart+k >= h.oldStart && ip.oldStart+k < hunkOldEnd
				case '+':
					in = ip.newStart+k >= h.newStart && ip.newStart+k < hunkNewEnd
				default:
					in = ip.oldStart+k >= h.oldStart && ip.oldStart+k < hunkOldEnd
				}
				if !in {
					continue
				}
				if ip.part.op == '-' {
					l = "-" + l
				} else if ip.part.op == '+' {
					l = "+" + l
				}
				h.lines = append(h.lines, l)
			}
		}

		// Prefix context lines with a space.
		for k, l := range h.lines {
			if l != "" && l[0] != '-' && l[0] != '+' {
				h.lines[k] = " " + l
			}
		}
		hunks = append(hunks, h)
	}

	var b strings.Builder
	b.WriteString("--- " + path + "\n")
	b.WriteString("+++ " + path + "\n")

	for _, h := range hunks {
		oldStart := h.oldStart + 1
		newStart := h.newStart + 1
		if h.oldCount == 0 {
			oldStart = h.oldStart
		}
		if h.newCount == 0 {
			newStart = h.newStart
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, h.oldCount, newStart, h.newCount)
		for _, l := range h.lines {
			b.WriteString(l + "\n")
		}
	}

	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
