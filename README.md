# tau-tool

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![MCP](https://img.shields.io/badge/Model%20Context%20Protocol-1.7.0-000000)](https://modelcontextprotocol.io/)
[![Build](https://img.shields.io/badge/build-passing-brightgreen)](#development)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen)](#testing)
[![Tools](https://img.shields.io/badge/tools-6-8A2BE2)](#tools)

An MCP server in Go. It talks to any MCP client over stdio and gives the agent file tools most clients don't have: `read`, `write`, `edit`, `bash` (ported from the [pi coding agent](https://github.com/mariozechner/pi-coding-agent)), plus a `settings` tool for changing its own config and a `websearch` that needs no API key.

## Why "tau"

τ is π cut in half. This project keeps pi's file tools but not pi's model providers, so "half of pi" is about right.

## Features

- `read` / `write` / `edit` / `bash`, the pi file tools, one per file
- `settings` for get/set/unset of agent config, persisted to disk and effective immediately
- per-file `encoding` on read/write/edit, and a `shellEncoding` setting for bash output, so GBK-era files and shells don't garble Chinese
- `websearch` over a three-endpoint DuckDuckGo fallback (API, html, lite) with retry
- no command injection: encodings are set by the agent, not bolted onto your commands

## Tools

| Tool | What it does |
|---|---|
| `read` | File contents (text or image), `offset`/`limit` paging, truncates at 2000 lines / 50KB and tells you how to continue |
| `write` | Create or overwrite a file, parent dirs included |
| `edit` | Exact text replacement, several disjoint edits in one call, fuzzy match, keeps CRLF/BOM, returns a unified patch |
| `bash` | Run a command (pwsh, nu, cmd, bash...); timeout/abort kills the process tree; truncated output goes to a temp file |
| `settings` | `get` / `set` / `unset` agent config; the tool description is generated from the same list |
| `websearch` | DuckDuckGo search without a key, returns `{title, url, abstract}` |

## Quick start

```bash
make build        # bin/tau-tool (bin/tau-tool.exe on Windows)
```

Register it as a stdio server:

```json
{
  "mcpServers": {
    "tau-tool": {
      "type": "stdio",
      "command": "/path/to/tau-tool/bin/tau-tool",
      "args": []
    }
  }
}
```

On Windows the binary is `tau-tool.exe`.

## Configuration

The `settings` tool is how you (or the agent) change things:

```
settings set shellPath pwsh              # bash tool uses pwsh; bare names resolve via PATH
settings set shellCommandPrefix "set -e" # prepended to every command
settings set shellEncoding gbk           # decode shell output as GBK
settings get shellPath
settings unset shellPath
```

Stored in `~/.tau-tool/settings.json`, or wherever `TAU_TOOL_SETTINGS` points.

### Environment variables

| Variable | Purpose | Default |
|---|---|---|
| `TAU_TOOL_CWD` | Working directory for relative paths | process cwd |
| `TAU_TOOL_SETTINGS` | Settings file path | `~/.tau-tool/settings.json` |

## Encoding

`read`, `write`, `edit` take an optional `encoding`: `utf-8` (default), `gbk`, `gb18030`, `big5`, `shift-jis`, `euc-jp`, `euc-kr`, `latin1`, `windows-1252`.

For bash, set `shellEncoding`. On Unix, shells emit UTF-8 and nothing needs setting. On Windows it's less predictable: the console shares one code page that other programs can flip (WSL sets it to 65001), so what a shell actually emits is not stable. That's why the agent states the encoding instead of the tool injecting `chcp`.

## Development

```bash
make build    # compile to bin/
make run      # build and run (stdio)
make test     # all tests
make vet      # go vet
make fmt      # go fmt
make clean    # remove bin/
```

## Testing

`go test ./...` covers truncation, diff, edit matching and errors, encoding round-trips, websearch parsing/fallback/retry, full MCP sessions in memory, and an e2e that spawns the real binary over stdio (settings go to a temp file, so your config stays untouched).

## Dependencies

- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) - MCP SDK
- [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) - HTML parsing (websearch)
- [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) - encoding support
