package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReadInput matches pi's read schema: path, offset (1-indexed), limit.
type ReadInput struct {
	Path     string `json:"path" jsonschema:"Path to the file to read (relative or absolute)"`
	Offset   *int   `json:"offset,omitempty" jsonschema:"Line number to start reading from (1-indexed)"`
	Limit    *int   `json:"limit,omitempty" jsonschema:"Maximum number of lines to read"`
	Encoding string `json:"encoding,omitempty" jsonschema:"Text encoding of the file: utf-8 (default), gbk, gb18030, big5, shift-jis, euc-jp, euc-kr, latin1, windows-1252"`
}

// ReadOperations lets the read tool delegate file reading to remote systems.
type ReadOperations interface {
	ReadFile(absolutePath string) ([]byte, error)
	Access(absolutePath string) error
	DetectImageMimeType(absolutePath string) (string, error)
}

type localReadOperations struct{}

func (localReadOperations) ReadFile(absolutePath string) ([]byte, error) {
	return os.ReadFile(absolutePath)
}

func (localReadOperations) Access(absolutePath string) error {
	f, err := os.Open(absolutePath)
	if err != nil {
		return err
	}
	return f.Close()
}

func (localReadOperations) DetectImageMimeType(absolutePath string) (string, error) {
	return detectImageMimeType(absolutePath)
}

var supportedImageExtensions = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

func detectImageMimeType(absolutePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(absolutePath))
	if mime, ok := supportedImageExtensions[ext]; ok {
		return mime, nil
	}
	return "", nil
}

// ReadToolDescription is pi's exact read tool description.
const ReadToolDescription = "Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete."

// ReadToolPromptSnippet and guidelines mirror pi's system prompt contributions.
const (
	ReadToolPromptSnippet = "Read file contents"
	ReadToolGuideline     = "Use read to examine files instead of cat or sed."
)

type readDeps struct {
	ops ReadOperations
}

// CreateReadTool returns the read tool handler.
func CreateReadTool(ops ...ReadOperations) mcp.ToolHandlerFor[ReadInput, any] {
	deps := readDeps{}
	if len(ops) > 0 && ops[0] != nil {
		deps.ops = ops[0]
	} else {
		deps.ops = localReadOperations{}
	}
	return deps.handle
}

func (d readDeps) handle(_ context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
	if in.Path == "" {
		return toolError("read: path is required"), nil, nil
	}

	absolutePath := resolveReadPath(in.Path)

	if err := d.ops.Access(absolutePath); err != nil {
		return toolError(err.Error()), nil, nil
	}

	mimeType, _ := d.ops.DetectImageMimeType(absolutePath)
	if mimeType != "" {
		return d.readImage(absolutePath, mimeType)
	}

	return d.readText(in, absolutePath)
}

func (d readDeps) readImage(absolutePath, mimeType string) (*mcp.CallToolResult, any, error) {
	data, err := d.ops.ReadFile(absolutePath)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}
	note := fmt.Sprintf("Read image file [%s]", mimeType)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: note},
			&mcp.ImageContent{Data: data, MIMEType: mimeType},
		},
	}, nil, nil
}

func (d readDeps) readText(in ReadInput, absolutePath string) (*mcp.CallToolResult, any, error) {
	data, err := d.ops.ReadFile(absolutePath)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	textContent, err := decodeBytes(data, in.Encoding)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}
	allLines := strings.Split(textContent, "\n")
	totalFileLines := len(allLines)

	startLine := 0
	if in.Offset != nil && *in.Offset > 0 {
		startLine = *in.Offset - 1
	}
	if startLine < 0 {
		startLine = 0
	}
	startLineDisplay := startLine + 1

	if startLine >= len(allLines) {
		return toolError(fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", *in.Offset, len(allLines))), nil, nil
	}

	var selectedContent string
	var userLimitedLines int
	userLimited := false
	if in.Limit != nil {
		endLine := startLine + *in.Limit
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		selectedContent = strings.Join(allLines[startLine:endLine], "\n")
		userLimitedLines = endLine - startLine
		userLimited = true
	} else {
		selectedContent = strings.Join(allLines[startLine:], "\n")
	}

	truncation := truncateHead(selectedContent, TruncationOptions{})
	var outputText string
	if truncation.FirstLineExceedsLimit {
		firstLineSize := FormatSize(len(allLines[startLine]))
		outputText = fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLineDisplay, firstLineSize, FormatSize(DEFAULT_MAX_BYTES), startLineDisplay, in.Path, DEFAULT_MAX_BYTES)
	} else if truncation.Truncated {
		endLineDisplay := startLineDisplay + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		outputText = truncation.Content
		if truncation.TruncatedBy == "lines" {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startLineDisplay, endLineDisplay, totalFileLines, nextOffset)
		} else {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				startLineDisplay, endLineDisplay, totalFileLines, FormatSize(DEFAULT_MAX_BYTES), nextOffset)
		}
	} else if userLimited && startLine+userLimitedLines < len(allLines) {
		remaining := len(allLines) - (startLine + userLimitedLines)
		nextOffset := startLine + userLimitedLines + 1
		outputText = fmt.Sprintf("%s\n\n[%d more lines in file. Use offset=%d to continue.]",
			truncation.Content, remaining, nextOffset)
	} else {
		outputText = truncation.Content
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: outputText}},
	}, nil, nil
}

func toolError(text string) *mcp.CallToolResult {
	if text == "" {
		text = "unknown error"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}
