package tools

import (
	"strings"
	"testing"
)

func TestFormatSize(t *testing.T) {
	cases := []struct {
		bytes int
		want  string
	}{
		{0, "0B"},
		{500, "500B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{50 * 1024, "50.0KB"},
		{1024 * 1024, "1.0MB"},
	}
	for _, c := range cases {
		if got := FormatSize(c.bytes); got != c.want {
			t.Errorf("FormatSize(%d) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

func TestTruncateHeadNoTruncation(t *testing.T) {
	content := "a\nb\nc"
	res := truncateHead(content, TruncationOptions{})
	if res.Truncated {
		t.Fatalf("expected no truncation")
	}
	if res.Content != content {
		t.Errorf("content = %q, want %q", res.Content, content)
	}
	if res.TotalLines != 3 {
		t.Errorf("TotalLines = %d, want 3", res.TotalLines)
	}
	if res.TotalBytes != len(content) {
		t.Errorf("TotalBytes = %d, want %d", res.TotalBytes, len(content))
	}
}

func TestTruncateHeadLineLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "line")
	}
	content := strings.Join(lines, "\n")
	res := truncateHead(content, TruncationOptions{MaxLines: 3})
	if !res.Truncated {
		t.Fatalf("expected truncation")
	}
	if res.TruncatedBy != "lines" {
		t.Errorf("TruncatedBy = %q, want lines", res.TruncatedBy)
	}
	if res.OutputLines != 3 {
		t.Errorf("OutputLines = %d, want 3", res.OutputLines)
	}
	if res.Content != "line\nline\nline" {
		t.Errorf("Content = %q", res.Content)
	}
	if res.TotalLines != 10 {
		t.Errorf("TotalLines = %d, want 10", res.TotalLines)
	}
}

func TestTruncateHeadByteLimit(t *testing.T) {
	content := "aaaa\nbbbb\ncccc\ndddd"
	res := truncateHead(content, TruncationOptions{MaxBytes: 9})
	if !res.Truncated {
		t.Fatalf("expected truncation")
	}
	if res.TruncatedBy != "bytes" {
		t.Errorf("TruncatedBy = %q, want bytes", res.TruncatedBy)
	}
	// 4 + 1 + 4 = 9 bytes fits exactly two lines.
	if res.Content != "aaaa\nbbbb" {
		t.Errorf("Content = %q, want %q", res.Content, "aaaa\nbbbb")
	}
}

func TestTruncateHeadFirstLineExceeds(t *testing.T) {
	content := strings.Repeat("x", 100) + "\nafter"
	res := truncateHead(content, TruncationOptions{MaxBytes: 50})
	if !res.FirstLineExceedsLimit {
		t.Errorf("expected FirstLineExceedsLimit")
	}
	if res.Content != "" {
		t.Errorf("Content = %q, want empty", res.Content)
	}
}

func TestTruncateTailKeepsEnd(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "line")
	}
	content := strings.Join(lines, "\n")
	res := truncateTail(content, TruncationOptions{MaxLines: 3})
	if !res.Truncated {
		t.Fatalf("expected truncation")
	}
	if res.Content != "line\nline\nline" {
		t.Errorf("Content = %q", res.Content)
	}
}

func TestTruncateTailByteLimitEdgePartial(t *testing.T) {
	content := strings.Repeat("x", 100)
	res := truncateTail(content, TruncationOptions{MaxBytes: 50})
	if !res.Truncated {
		t.Fatalf("expected truncation")
	}
	if !res.LastLinePartial {
		t.Errorf("expected LastLinePartial")
	}
	if len(res.Content) != 50 {
		t.Errorf("Content length = %d, want 50", len(res.Content))
	}
}

func TestTruncateTailTrailingNewlineCounting(t *testing.T) {
	content := "a\nb\nc\n"
	res := truncateTail(content, TruncationOptions{})
	if res.TotalLines != 3 {
		t.Errorf("TotalLines = %d, want 3", res.TotalLines)
	}
}

func TestTruncateLine(t *testing.T) {
	text, truncated := truncateLine("short", 10)
	if truncated || text != "short" {
		t.Errorf("unexpected: %q %v", text, truncated)
	}
	text, truncated = truncateLine(strings.Repeat("x", 20), 5)
	if !truncated {
		t.Errorf("expected truncated")
	}
	want := "xxxxx... [truncated]"
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}
