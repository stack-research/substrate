package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stack-research/substrate/internal/lifecycle"
	"github.com/stack-research/substrate/internal/mcpserver"
	"github.com/stack-research/substrate/internal/substrate"
	"github.com/stack-research/substrate/internal/version"
)

type pathsFlag []string

func (p *pathsFlag) String() string         { return fmt.Sprint([]string(*p)) }
func (p *pathsFlag) Set(value string) error { *p = append(*p, value); return nil }

func main() {
	var spaces pathsFlag
	var registryFile, rawName string
	var showVersion bool
	flag.Var(&spaces, "space", "space directory; repeat for several")
	flag.StringVar(&registryFile, "spaces-file", "", "space registry file")
	flag.StringVar(&rawName, "name", "", "default participant")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()
	if showVersion {
		fmt.Printf("substrate-mcp %s (%s)\n", version.Version, version.Runtime)
		return
	}
	var actor *substrate.Name
	if rawName != "" {
		parsed, err := substrate.ParseName(rawName)
		if err != nil {
			fatal(err)
		}
		actor = &parsed
	}
	logDir := filepath.Join(substrate.HomeDir(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fatal(err)
	}
	logName := "no-default"
	if actor != nil {
		logName = actor.ToPathComponent()
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "mcp-"+logName+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fatal(err)
	}
	defer logFile.Close()
	logger := slog.New(slog.NewTextHandler(logFile, nil))
	service := mcpserver.New(mcpserver.SpaceSource{Paths: spaces, RegistryFile: registryFile}, actor, logger)
	ctx, cancel := lifecycle.SignalContext(context.Background())
	defer cancel()
	if err := service.Server().Run(ctx, &mcp.StdioTransport{}); err != nil {
		// The go-sdk wraps the EOF of a client hanging up in a plain string, so
		// errors.Is alone cannot recognize this normal shutdown path.
		if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "server is closing: EOF") {
			fatal(err)
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "substrate-mcp:", err); os.Exit(1) }
