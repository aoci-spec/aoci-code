package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// NewMCPServer assembles the repository's MCP server, the same nine tools
// aoci mcp serves, under the engine version. The caller chooses the
// transport; Connect hosts it in process and ServeStdio serves stdio.
func (r *Repository) NewMCPServer() (*mcp.Server, error) {
	server, err := mcptools.NewServer(r.root, Version())
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return server, nil
}

// ServeStdio runs the repository's MCP server over the process's standard
// streams until ctx ends, exactly as aoci mcp does: stdout carries JSON-RPC
// only, so the calling program must keep its own output on stderr.
func (r *Repository) ServeStdio(ctx context.Context) error {
	if err := mcptools.RunStdio(ctx, r.root, Version()); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	return nil
}

// Session is an in-process MCP session on the repository: a client and a
// server connected over memory, so every call runs the tool's own handler
// and returns the tool's own result.
type Session struct {
	client *mcp.ClientSession
	server *mcp.ServerSession
}

// ToolResult is one tool call's outcome: the text the tool returned (for the
// write tools a JSON object, for the read tools the documented text
// contract), whether the tool reported an error, and the _meta the server
// attached.
type ToolResult struct {
	Text    string
	IsError bool
	Meta    map[string]any
}

// Connect opens an in-process session on the repository. Close it when done.
func (r *Repository) Connect(ctx context.Context) (*Session, error) {
	server, err := r.NewMCPServer()
	if err != nil {
		return nil, err
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "aoci-engine", Version: Version()}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &Session{client: clientSession, server: serverSession}, nil
}

// Tools lists the tool names the session serves, in the server's order.
func (s *Session) Tools(ctx context.Context) ([]string, error) {
	result, err := s.client.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if tool != nil {
			names = append(names, tool.Name)
		}
	}
	return names, nil
}

// Call executes one of the nine tools with args, which must marshal to the
// tool's input schema (a map or a struct). A tool refusal comes back as a
// result with IsError set, not as an error; an error means the call itself
// could not be made.
func (s *Session) Call(ctx context.Context, tool string, args any) (*ToolResult, error) {
	if strings.TrimSpace(tool) == "" {
		return nil, errors.New("engine: tool name is required")
	}
	if args == nil {
		args = map[string]any{}
	}
	result, err := s.client.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	var text strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text.WriteString(block.Text)
		}
	}
	out := &ToolResult{Text: text.String(), IsError: result.IsError}
	if len(result.Meta) > 0 {
		out.Meta = map[string]any(result.Meta)
	}
	return out, nil
}

// Close ends the session on both sides.
func (s *Session) Close() error {
	var errs []error
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.server != nil {
		if err := s.server.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
