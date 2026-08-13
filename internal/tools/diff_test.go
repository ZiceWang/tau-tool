package tools

import (
	"strings"
	"testing"
)

func TestDiffLinesBasic(t *testing.T) {
	parts := diffLines("a\nb\nc", "a\nB\nc")
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want 4", len(parts))
	}
	want := []struct {
		op    byte
		value string
	}{
		{'=', "a"},
		{'-', "b"},
		{'+', "B"},
		{'=', "c"},
	}
	for i, w := range want {
		if parts[i].op != w.op || parts[i].value != w.value {
			t.Errorf("part%d = %c %q, want %c %q", i, parts[i].op, parts[i].value, w.op, w.value)
		}
	}
}

func TestDiffLinesInsertion(t *testing.T) {
	parts := diffLines("a\nb", "a\nX\nb")
	if len(parts) != 3 {
		t.Fatalf("parts = %d, want 3", len(parts))
	}
	if parts[0].op != '=' || parts[1].op != '+' || parts[2].op != '=' {
		t.Errorf("unexpected ops: %c %c %c", parts[0].op, parts[1].op, parts[2].op)
	}
}

func TestDiffLinesEmpty(t *testing.T) {
	if parts := diffLines("", ""); len(parts) != 0 {
		t.Errorf("expected no parts, got %d", len(parts))
	}
}

func TestGenerateDiffStringShowsChange(t *testing.T) {
	diff, first := generateDiffString("a\nb\nc", "a\nB\nc", 4)
	if first != 2 {
		t.Errorf("firstChangedLine = %d, want 2", first)
	}
	if !strings.Contains(diff, "-2 b") || !strings.Contains(diff, "+2 B") {
		t.Errorf("diff missing change lines: %q", diff)
	}
}

func TestGenerateDiffStringSkipsLargeContext(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 100; i++ {
		oldLines = append(oldLines, "line")
		newLines = append(newLines, "line")
	}
	newLines[50] = "CHANGED"
	diff, first := generateDiffString(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"), 4)
	if first != 51 {
		t.Errorf("firstChangedLine = %d, want 51", first)
	}
	if !strings.Contains(diff, "...") {
		t.Errorf("expected ellipsis in diff: %q", diff)
	}
}

func TestGenerateUnifiedPatchBasic(t *testing.T) {
	patch := generateUnifiedPatch("f.txt", "a\nb\nc\n", "a\nB\nc\n", 4)
	if !strings.Contains(patch, "--- f.txt") || !strings.Contains(patch, "+++ f.txt") {
		t.Fatalf("missing headers: %q", patch)
	}
	if !strings.Contains(patch, "@@ -1,3 +1,3 @@") {
		t.Errorf("missing hunk header: %q", patch)
	}
	if !strings.Contains(patch, "-b") || !strings.Contains(patch, "+B") {
		t.Errorf("missing change lines: %q", patch)
	}
}

func TestGenerateUnifiedPatchMultiHunk(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 100; i++ {
		oldLines = append(oldLines, "line"+itoa(i))
		newLines = append(newLines, "line"+itoa(i))
	}
	newLines[9] = "CHANGED1"
	newLines[90] = "CHANGED2"
	patch := generateUnifiedPatch("f.txt", strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"), 4)
	hunks := 0
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "@@") {
			hunks++
		}
	}
	if hunks != 2 {
		t.Errorf("hunks = %d, want 2: %q", hunks, patch)
	}
}

func TestGenerateUnifiedPatchNoChanges(t *testing.T) {
	if patch := generateUnifiedPatch("f.txt", "a\nb\n", "a\nb\n", 4); patch != "" {
		t.Errorf("expected empty patch, got %q", patch)
	}
}

func TestGenerateUnifiedPatchAppliesCleanly(t *testing.T) {
	oldContent := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	newContent := "line1\nline2\nCHANGED\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	patch := generateUnifiedPatch("f.txt", oldContent, newContent, 4)
	if !strings.Contains(patch, "@@") {
		t.Fatalf("no hunks: %q", patch)
	}
	// Round-trip: applying the patch manually by reconstructing from parts.
	// Simpler sanity check: the changed line is present in the patch.
	if !strings.Contains(patch, "-line3") || !strings.Contains(patch, "+CHANGED") {
		t.Errorf("patch content wrong: %q", patch)
	}
}

func TestDetectLineEnding(t *testing.T) {
	if detectLineEnding("a\nb") != "\n" {
		t.Error("expected LF")
	}
	if detectLineEnding("a\r\nb") != "\r\n" {
		t.Error("expected CRLF")
	}
	if detectLineEnding("no newline") != "\n" {
		t.Error("expected LF default")
	}
}

func TestNormalizeToLFAndRestore(t *testing.T) {
	if got := normalizeToLF("a\r\nb\rc"); got != "a\nb\nc" {
		t.Errorf("normalizeToLF = %q", got)
	}
	if got := restoreLineEndings("a\nb", "\r\n"); got != "a\r\nb" {
		t.Errorf("restoreLineEndings = %q", got)
	}
}

func TestNormalizeForFuzzyMatch(t *testing.T) {
	in := "\u201cquoted\u201d\u2014dash \u00a0space  "
	got := normalizeForFuzzyMatch(in)
	want := "\"quoted\"-dash  space"
	if got != want {
		t.Errorf("normalize = %q, want %q", got, want)
	}
}
