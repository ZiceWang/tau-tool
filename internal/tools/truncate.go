package tools

import (
	"strings"
	"unicode/utf8"
)

// Truncation limits shared across tool outputs.
// Truncation is based on two independent limits - whichever is hit first wins:
//   - Line limit (default: 2000 lines)
//   - Byte limit (default: 50KB)
//
// Never returns partial lines (except the bash tail truncation edge case).
const (
	DEFAULT_MAX_LINES = 2000
	DEFAULT_MAX_BYTES = 50 * 1024 // 50KB
	GREP_MAX_LINE_LENGTH = 500 // Max chars per grep match line
)

type TruncationResult struct {
	Content string

	Truncated    bool
	TruncatedBy  string // "lines", "bytes", or "" if not truncated
	TotalLines   int
	TotalBytes   int
	OutputLines  int
	OutputBytes  int
	LastLinePartial     bool
	FirstLineExceedsLimit bool
	MaxLines      int
	MaxBytes      int
}

type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

// Format bytes as human-readable size.
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return intToString(bytes) + "B"
	case bytes < 1024*1024:
		return formatFloat(float64(bytes)/1024) + "KB"
	default:
		return formatFloat(float64(bytes)/(1024*1024)) + "MB"
	}
}

func intToString(n int) string {
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// formatFloat mimics JS toFixed(1): one decimal place.
func formatFloat(v float64) string {
	scaled := v * 10
	rounded := int(scaled + 0.5)
	intPart := rounded / 10
	fracPart := rounded % 10
	return itoa(intPart) + "." + itoa(fracPart)
}

// splitLinesForCounting splits content into lines, dropping the trailing empty
// element introduced by a final newline (JS "".split behavior parity).
func splitLinesForCounting(content string) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func byteLength(s string) int {
	return len(s)
}

// Truncate content from the head (keep first N lines/bytes).
// Suitable for file reads where you want to see the beginning.
// Never returns partial lines. If first line exceeds byte limit,
// returns empty content with FirstLineExceedsLimit=true.
func truncateHead(content string, options TruncationOptions) TruncationResult {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DEFAULT_MAX_LINES
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DEFAULT_MAX_BYTES
	}

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{Content: content, Truncated: false, TruncatedBy: "", TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: totalLines, OutputBytes: totalBytes, MaxLines: maxLines, MaxBytes: maxBytes}
	}

	firstLineBytes := 0
	if totalLines > 0 {
		firstLineBytes = len(lines[0])
	}
	if firstLineBytes > maxBytes {
		return TruncationResult{Content: "", Truncated: true, TruncatedBy: "bytes", TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: 0, OutputBytes: 0, FirstLineExceedsLimit: true, MaxLines: maxLines, MaxBytes: maxBytes}
	}

	outputLinesArr := []string{}
	outputBytesCount := 0
	truncatedBy := "lines"

	for i := 0; i < len(lines) && i < maxLines; i++ {
		line := lines[i]
		lineBytes := len(line)
		if i > 0 {
			lineBytes++ // +1 for newline
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		outputLinesArr = append(outputLinesArr, line)
		outputBytesCount += lineBytes
	}

	if len(outputLinesArr) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}

	outputContent := strings.Join(outputLinesArr, "\n")

	return TruncationResult{Content: outputContent, Truncated: true, TruncatedBy: truncatedBy, TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: len(outputLinesArr), OutputBytes: len(outputContent), MaxLines: maxLines, MaxBytes: maxBytes}
}

// Truncate content from the tail (keep last N lines/bytes).
// Suitable for bash output where you want to see the end (errors, final results).
// May return partial first line if the last line of original content exceeds byte limit.
func truncateTail(content string, options TruncationOptions) TruncationResult {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DEFAULT_MAX_LINES
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DEFAULT_MAX_BYTES
	}

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{Content: content, Truncated: false, TruncatedBy: "", TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: totalLines, OutputBytes: totalBytes, MaxLines: maxLines, MaxBytes: maxBytes}
	}

	outputLinesArr := []string{}
	outputBytesCount := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(outputLinesArr) < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(outputLinesArr) > 0 {
			lineBytes++ // +1 for newline
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(outputLinesArr) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				outputLinesArr = append([]string{truncatedLine}, outputLinesArr...)
				outputBytesCount = len(truncatedLine)
				lastLinePartial = true
			}
			break
		}
		outputLinesArr = append([]string{line}, outputLinesArr...)
		outputBytesCount += lineBytes
	}

	if len(outputLinesArr) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}

	outputContent := strings.Join(outputLinesArr, "\n")

	return TruncationResult{Content: outputContent, Truncated: true, TruncatedBy: truncatedBy, TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: len(outputLinesArr), OutputBytes: len(outputContent), LastLinePartial: lastLinePartial, MaxLines: maxLines, MaxBytes: maxBytes}
}

// truncateStringToBytesFromEnd truncates a string to fit within a byte limit
// (from the end), handling multi-byte UTF-8 characters correctly.
func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	start := len(b) - maxBytes
	for start < len(b) && (b[start]&0xc0) == 0x80 {
		start++
	}
	return string(b[start:])
}

// Truncate a single line to max characters, adding "[truncated]" suffix.
// Used for grep match lines.
func truncateLine(line string, maxChars int) (string, bool) {
	if maxChars == 0 {
		maxChars = GREP_MAX_LINE_LENGTH
	}
	if utf8.RuneCountInString(line) <= maxChars {
		return line, false
	}
	runes := []rune(line)
	return string(runes[:maxChars]) + "... [truncated]", true
}
