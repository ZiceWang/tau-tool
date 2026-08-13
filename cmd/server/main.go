package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/server"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "http":
			runHTTP(args[1:])
			return
		case "help", "--help", "-h":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
			printUsage()
			os.Exit(1)
		}
	}
	runStdio()
}

func runStdio() {
	if err := server.New().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func printUsage() {
	fmt.Println(`tau-tool - MCP server

Usage:
  tau-tool              run the stdio MCP server (default)
  tau-tool http         run the streamable HTTP MCP server
      --host <host>     host to bind (default localhost)
      --port <n>        port to listen on (default 8899)
  tau-tool help         show this help`)
}
