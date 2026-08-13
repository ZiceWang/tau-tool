package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/tools"
)

const (
	Name    = "tau-tool"
	Version = "v0.1.0"
)

// New creates the MCP server with pi's core tools (read, write, edit, bash)
// plus a settings tool. Tools operate on the process working directory.
func New() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    Name,
		Title:   "tau-tool",
		Version: Version,
	}, nil)

	settings := tools.NewSettingsStore()

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read",
		Description: tools.ReadToolDescription,
	}, tools.CreateReadTool())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write",
		Description: tools.WriteToolDescription,
	}, tools.CreateWriteTool())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "edit",
		Description: tools.EditToolDescription,
	}, tools.CreateEditTool())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "bash",
		Description: tools.BashToolDescription,
	}, tools.CreateBashTool(tools.BashToolOptions{Settings: settings}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "settings",
		Description: tools.SettingsToolDescription(settings),
	}, tools.CreateSettingsTool(settings))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "websearch",
		Description: tools.WebSearchToolDescription,
	}, tools.CreateWebSearchTool())

	return server
}
