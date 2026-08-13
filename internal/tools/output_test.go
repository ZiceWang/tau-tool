package tools

import (
	"os"
	"strings"
	"testing"
)

func TestOutputAccumulatorSmallOutput(t *testing.T) {
	acc := NewOutputAccumulator(OutputAccumulatorOptions{})
	if err := acc.Append([]byte("hello\nworld")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := acc.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	snap, err := acc.Snapshot(true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Content != "hello\nworld" {
		t.Errorf("content = %q", snap.Content)
	}
	if snap.Truncation.Truncated {
		t.Errorf("unexpected truncation")
	}
	if snap.FullOutputPath != "" {
		t.Errorf("unexpected temp file: %q", snap.FullOutputPath)
	}
}

func TestOutputAccumulatorLargeOutputSpills(t *testing.T) {
	acc := NewOutputAccumulator(OutputAccumulatorOptions{MaxLines: 5, MaxBytes: 100})
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	big := strings.Join(lines, "\n")
	if err := acc.Append([]byte(big)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := acc.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	snap, err := acc.Snapshot(true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.Truncation.Truncated {
		t.Fatalf("expected truncation")
	}
	if snap.FullOutputPath == "" {
		t.Fatalf("expected temp file path")
	}
	if !snap.Truncation.LastLinePartial && snap.Truncation.TruncatedBy != "lines" && snap.Truncation.TruncatedBy != "bytes" {
		t.Errorf("truncatedBy = %q", snap.Truncation.TruncatedBy)
	}
	// The temp file should contain the full output.
	data, err := os.ReadFile(snap.FullOutputPath)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(data) != big {
		t.Errorf("temp file content mismatch: len %d vs %d", len(data), len(big))
	}
	_ = acc.CloseTempFile()
}

func TestOutputAccumulatorLineCounting(t *testing.T) {
	acc := NewOutputAccumulator(OutputAccumulatorOptions{})
	if err := acc.Append([]byte("a\nb\nc\n")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := acc.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	snap, _ := acc.Snapshot(true)
	if snap.Truncation.TotalLines != 3 {
		t.Errorf("TotalLines = %d, want 3", snap.Truncation.TotalLines)
	}
}

func TestOutputAccumulatorAppendAfterFinish(t *testing.T) {
	acc := NewOutputAccumulator(OutputAccumulatorOptions{})
	_ = acc.Finish()
	if err := acc.Append([]byte("x")); err == nil {
		t.Errorf("expected error appending to finished accumulator")
	}
}

func TestFormatBashOutputEmpty(t *testing.T) {
	text, details := formatBashOutput(OutputSnapshot{Content: ""}, 0)
	if text != "(no output)" {
		t.Errorf("text = %q", text)
	}
	if details.FullOutputPath != "" {
		t.Errorf("details = %+v", details)
	}
}

func TestFormatBashOutputTruncated(t *testing.T) {
	truncation := TruncationResult{
		Truncated:   true,
		TruncatedBy: "lines",
		TotalLines:  100,
		OutputLines: 5,
	}
	snap := OutputSnapshot{
		Content:        "tail",
		Truncation:     truncation,
		FullOutputPath: "/tmp/pi-bash.log",
	}
	text, details := formatBashOutput(snap, 0)
	if !strings.Contains(text, "Showing lines 96-100 of 100") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "/tmp/pi-bash.log") {
		t.Errorf("text missing path: %q", text)
	}
	if details.FullOutputPath != "/tmp/pi-bash.log" {
		t.Errorf("details = %+v", details)
	}
}
