package tools

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// OutputAccumulatorOptions mirrors pi's OutputAccumulator options.
type OutputAccumulatorOptions struct {
	MaxLines       int
	MaxBytes       int
	TempFilePrefix string
}

// OutputSnapshot is a point-in-time view of accumulated output.
type OutputSnapshot struct {
	Content        string
	Truncation     TruncationResult
	FullOutputPath string
}

// OutputAccumulator incrementally tracks streaming output with bounded memory.
// It keeps only a decoded tail for display snapshots and opens a temp file
// when the full output must be preserved.
type OutputAccumulator struct {
	mu                 sync.Mutex
	maxLines           int
	maxBytes           int
	maxRollingBytes    int
	tempFilePrefix     string

	rawChunks   []byte
	tailText    string
	tailBytes   int
	tailStartsAtLineBoundary bool
	totalRawBytes   int
	totalDecodedBytes int
	completedLines  int
	totalLines      int
	currentLineBytes int
	hasOpenLine     bool
	finished        bool

	tempFilePath string
	tempFile     *os.File
}

// NewOutputAccumulator creates an accumulator with pi's defaults (2000 lines,
// 50KB), spilling to a temp file when output exceeds the limits.
func NewOutputAccumulator(options OutputAccumulatorOptions) *OutputAccumulator {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DEFAULT_MAX_LINES
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DEFAULT_MAX_BYTES
	}
	maxRolling := maxBytes * 2
	if maxRolling < 1 {
		maxRolling = 1
	}
	prefix := options.TempFilePrefix
	if prefix == "" {
		prefix = "pi-output"
	}
	return &OutputAccumulator{
		maxLines:               maxLines,
		maxBytes:               maxBytes,
		maxRollingBytes:        maxRolling,
		tempFilePrefix:         prefix,
		tailStartsAtLineBoundary: true,
	}
}

// Append ingests a chunk of raw bytes.
func (o *OutputAccumulator) Append(data []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.finished {
		return errAccumulatorFinished
	}
	o.totalRawBytes += len(data)
	// Decode incrementally: strip invalid UTF-8 sequences and handle
	// multi-byte chars split across chunk boundaries via a running decoder.
	text, pending := decodeStream(data)
	o.appendDecodedText(text)

	if o.tempFile != nil || o.shouldUseTempFile() {
		if err := o.ensureTempFile(); err != nil {
			return err
		}
		if _, err := o.tempFile.Write(data); err != nil {
			return err
		}
	} else if len(data) > 0 {
		o.rawChunks = append(o.rawChunks, data...)
	}
	_ = pending
	return nil
}

// Finish finalizes the accumulator, flushing any pending decoder state.
func (o *OutputAccumulator) Finish() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.finishLocked()
}

func (o *OutputAccumulator) finishLocked() error {
	if o.finished {
		return nil
	}
	o.finished = true
	o.appendDecodedText(decodeFinal())
	if o.shouldUseTempFile() {
		return o.ensureTempFile()
	}
	return nil
}

// Snapshot returns the current bounded view of the output.
func (o *OutputAccumulator) Snapshot(persistIfTruncated bool) (OutputSnapshot, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshotLocked(persistIfTruncated)
}

func (o *OutputAccumulator) snapshotLocked(persistIfTruncated bool) (OutputSnapshot, error) {
	tailTruncation := truncateTail(o.getSnapshotText(), TruncationOptions{MaxLines: o.maxLines, MaxBytes: o.maxBytes})
	truncated := o.totalLines > o.maxLines || o.totalDecodedBytes > o.maxBytes
	truncatedBy := ""
	if truncated {
		if tailTruncation.TruncatedBy != "" {
			truncatedBy = tailTruncation.TruncatedBy
		} else if o.totalDecodedBytes > o.maxBytes {
			truncatedBy = "bytes"
		} else {
			truncatedBy = "lines"
		}
	}
	truncation := tailTruncation
	truncation.Truncated = truncated
	truncation.TruncatedBy = truncatedBy
	truncation.TotalLines = o.totalLines
	truncation.TotalBytes = o.totalDecodedBytes
	truncation.MaxLines = o.maxLines
	truncation.MaxBytes = o.maxBytes

	if persistIfTruncated && truncation.Truncated {
		if err := o.ensureTempFile(); err != nil {
			return OutputSnapshot{}, err
		}
	}

	return OutputSnapshot{Content: truncation.Content, Truncation: truncation, FullOutputPath: o.tempFilePath}, nil
}

// CloseTempFile closes and finalizes the spill temp file (no-op if unused).
func (o *OutputAccumulator) CloseTempFile() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tempFile == nil {
		return nil
	}
	err := o.tempFile.Close()
	o.tempFile = nil
	return err
}

// GetLastLineBytes reports the byte size of the currently open line.
func (o *OutputAccumulator) GetLastLineBytes() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.currentLineBytes
}

func (o *OutputAccumulator) appendDecodedText(text string) {
	if len(text) == 0 {
		return
	}
	bytes := len(text)
	o.totalDecodedBytes += bytes
	o.tailText += text
	o.tailBytes += bytes
	if o.tailBytes > o.maxRollingBytes*2 {
		o.trimTail()
	}

	newlines := 0
	lastNewline := -1
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			newlines++
			lastNewline = i
		}
	}
	if newlines == 0 {
		o.currentLineBytes += bytes
		o.hasOpenLine = true
	} else {
		o.completedLines += newlines
		tail := text[lastNewline+1:]
		o.currentLineBytes = len(tail)
		o.hasOpenLine = len(tail) > 0
	}
	o.totalLines = o.completedLines
	if o.hasOpenLine {
		o.totalLines++
	}
}

func (o *OutputAccumulator) trimTail() {
	buf := []byte(o.tailText)
	if len(buf) <= o.maxRollingBytes {
		o.tailBytes = len(buf)
		return
	}
	start := len(buf) - o.maxRollingBytes
	for start < len(buf) && (buf[start]&0xc0) == 0x80 {
		start++
	}
	if start == 0 {
		// keep existing flag
	} else {
		o.tailStartsAtLineBoundary = buf[start-1] == '\n'
	}
	o.tailText = string(buf[start:])
	o.tailBytes = len(o.tailText)
}

func (o *OutputAccumulator) getSnapshotText() string {
	if o.tailStartsAtLineBoundary {
		return o.tailText
	}
	firstNewline := strings.Index(o.tailText, "\n")
	if firstNewline == -1 {
		return o.tailText
	}
	return o.tailText[firstNewline+1:]
}

func (o *OutputAccumulator) shouldUseTempFile() bool {
	return o.totalRawBytes > o.maxBytes || o.totalDecodedBytes > o.maxBytes || o.totalLines > o.maxLines
}

func (o *OutputAccumulator) ensureTempFile() error {
	if o.tempFile != nil {
		return nil
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	path := filepath.Join(os.TempDir(), o.tempFilePrefix+"-"+hex.EncodeToString(id)+".log")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if len(o.rawChunks) > 0 {
		if _, err := f.Write(o.rawChunks); err != nil {
			_ = f.Close()
			return err
		}
		o.rawChunks = nil
	}
	o.tempFilePath = path
	o.tempFile = f
	return nil
}

var errAccumulatorFinished = &outputError{"Cannot append to a finished output accumulator"}

type outputError struct{ msg string }

func (e *outputError) Error() string { return e.msg }

// Streaming UTF-8 decode helpers: handle multi-byte characters split across
// chunk boundaries and drop invalid sequences (TextDecoder parity).
var utf8Pending []byte

func decodeStream(data []byte) (string, []byte) {
	if len(utf8Pending) > 0 {
		data = append(utf8Pending, data...)
		utf8Pending = nil
	}
	// Find the longest valid prefix; keep a possible incomplete trailing rune.
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	// Detect a truncated trailing rune: if the raw tail is an incomplete
	// sequence, re-decode without it.
	runes := []rune(text)
	if len(runes) > 0 && runes[len(runes)-1] == '\uFFFD' {
		// Check if the raw data ends with a partial multi-byte sequence.
		if i := incompleteUTF8Suffix(data); i > 0 {
			utf8Pending = append([]byte(nil), data[i:]...)
			text = strings.ToValidUTF8(string(data[:i]), "\uFFFD")
		}
	}
	return text, utf8Pending
}

func decodeFinal() string {
	p := utf8Pending
	utf8Pending = nil
	return strings.ToValidUTF8(string(p), "\uFFFD")
}

func incompleteUTF8Suffix(data []byte) int {
	// Scan back to find a partial UTF-8 sequence at the end.
	if len(data) == 0 {
		return 0
	}
	last := data[len(data)-1]
	if last < 0x80 {
		return 0
	}
	// Count leading continuation bytes of the last sequence.
	i := len(data) - 1
	for i >= 0 && (data[i]&0xc0) == 0x80 {
		i--
	}
	if i < 0 {
		return 0
	}
	lead := data[i]
	var expectedLen int
	switch {
	case lead&0xe0 == 0xc0:
		expectedLen = 2
	case lead&0xf0 == 0xe0:
		expectedLen = 3
	case lead&0xf8 == 0xf0:
		expectedLen = 4
	default:
		return 0
	}
	have := len(data) - i
	if have < expectedLen {
		return i
	}
	return 0
}
