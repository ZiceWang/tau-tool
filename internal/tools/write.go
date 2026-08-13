package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WriteInput matches pi's write schema: path, content.
type WriteInput struct {
	Path     string `json:"path" jsonschema:"Path to the file to write (relative or absolute)"`
	Content  string `json:"content" jsonschema:"Content to write to the file"`
	Encoding string `json:"encoding,omitempty" jsonschema:"Text encoding to write: utf-8 (default), gbk, gb18030, big5, shift-jis, euc-jp, euc-kr, latin1, windows-1252"`
}

// WriteOperations lets the write tool delegate file writing to remote systems.
type WriteOperations interface {
	WriteFile(absolutePath string, data []byte) error
	Mkdir(dir string) error
}

type localWriteOperations struct{}

func (localWriteOperations) WriteFile(absolutePath string, data []byte) error {
	return os.WriteFile(absolutePath, data, 0o644)
}

func (localWriteOperations) Mkdir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// WriteToolDescription is pi's exact write tool description.
const WriteToolDescription = "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."

// WriteToolPromptSnippet and guidelines mirror pi's system prompt contributions.
const (
	WriteToolPromptSnippet = "Create or overwrite files"
	WriteToolGuideline     = "Use write only for new files or complete rewrites."
)

type writeDeps struct {
	ops WriteOperations
}

// CreateWriteTool returns the write tool handler.
func CreateWriteTool(ops ...WriteOperations) mcp.ToolHandlerFor[WriteInput, any] {
	deps := writeDeps{}
	if len(ops) > 0 && ops[0] != nil {
		deps.ops = ops[0]
	} else {
		deps.ops = localWriteOperations{}
	}
	return deps.handle
}

func (d writeDeps) handle(_ context.Context, _ *mcp.CallToolRequest, in WriteInput) (*mcp.CallToolResult, any, error) {
	if in.Path == "" {
		return toolError("write: path is required"), nil, nil
	}

	absolutePath := resolveToCwd(in.Path)
	dir := filepath.Dir(absolutePath)

	data, err := encodeBytes(in.Content, in.Encoding)
	if err != nil {
		return toolError(err.Error()), nil, nil
	}

	var writeErr error
	err = withFileMutationQueue(absolutePath, func() error {
		// Create parent directories if needed.
		if err := d.ops.Mkdir(dir); err != nil {
			return fmt.Errorf("could not create parent directories for %s: %w", in.Path, err)
		}
		// Write the file contents.
		if err := d.ops.WriteFile(absolutePath, data); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		writeErr = err
	}

	if writeErr != nil {
		return toolError(writeErr.Error()), nil, nil
	}

	// JS string length semantics: count runes, matching pi's content.length.
	length := countRunes(in.Content)
	text := fmt.Sprintf("Successfully wrote %d bytes to %s", length, in.Path)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func countRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
