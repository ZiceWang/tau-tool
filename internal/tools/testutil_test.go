package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// textOf extracts the concatenated text content from a CallToolResult.
func textOf(result *mcp.CallToolResult) string {
	var b []byte
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, tc.Text...)
		}
	}
	return string(b)
}
