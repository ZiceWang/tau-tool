package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tau-tool/internal/server"
)

const (
	defaultHTTPPort = 8899
	defaultHost     = "localhost"
)

// runHTTP serves the MCP server over the streamable HTTP transport.
func runHTTP(args []string) {
	host := defaultHost
	port := defaultHTTPPort

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printUsage()
			return
		case arg == "--port" || strings.HasPrefix(arg, "--port="):
			val, err := flagValue(args, &i, arg, "--port")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			port, err = parsePort(val)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		case arg == "--host" || strings.HasPrefix(arg, "--host="):
			val, err := flagValue(args, &i, arg, "--host")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			host = val
		default:
			fmt.Fprintf(os.Stderr, "unknown argument: %s\n\n", arg)
			printUsage()
			os.Exit(1)
		}
	}

	// One server instance is shared by all sessions so the settings store stays
	// consistent across them.
	srv := server.New()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{})

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	httpServer := &http.Server{Addr: addr, Handler: mux}

	fmt.Printf("tau-tool http: listening on http://%s/mcp\n", addr)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

// flagValue returns the value for a --flag either as --flag=value or the next
// argument.
func flagValue(args []string, i *int, arg, name string) (string, error) {
	if v, ok := strings.CutPrefix(arg, name+"="); ok {
		return v, nil
	}
	if *i+1 < len(args) {
		*i++
		return args[*i], nil
	}
	return "", fmt.Errorf("missing value for %s", name)
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port: %s", s)
	}
	return p, nil
}
