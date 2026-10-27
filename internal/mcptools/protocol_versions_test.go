package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawMCP is a connection to the server that sends hand-built JSON-RPC, so a
// test can speak a revision, or send an answer, no SDK client would.
type rawMCP struct {
	t    *testing.T
	conn mcp.Connection
	next int
}

func newRawMCP(t *testing.T, root string) *rawMCP {
	t.Helper()
	server, err := NewServer(root, "raw-mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	conn, err := clientTransport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &rawMCP{t: t, conn: conn}
}

// call sends one request and returns its decoded result or its error.
func (r *rawMCP) call(method string, params map[string]any) (map[string]any, *jsonrpc.Error) {
	r.t.Helper()
	r.next++
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.next, "method": method, "params": params})
	if err != nil {
		r.t.Fatal(err)
	}
	request, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.conn.Write(context.Background(), request); err != nil {
		r.t.Fatal(err)
	}
	for {
		message, err := r.conn.Read(context.Background())
		if err != nil {
			r.t.Fatal(err)
		}
		response, ok := message.(*jsonrpc.Response)
		if !ok {
			continue
		}
		if response.Error != nil {
			var wire *jsonrpc.Error
			if !errors.As(response.Error, &wire) {
				r.t.Fatalf("%s failed without a JSON-RPC error: %v", method, response.Error)
			}
			return nil, wire
		}
		result := map[string]any{}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			r.t.Fatal(err)
		}
		return result, nil
	}
}

// sessionlessMeta is the per-request _meta a sessionless client sends.
func sessionlessMeta(revision string, capabilities map[string]any) map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    revision,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "raw-mcp-test", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": capabilities,
	}
}

// Every listed revision is reachable: an SDK client lands on the newest, a
// client pinned to the handshake era stays there, initialize never answers
// past the handshake's newest revision, and a sessionless request is served
// at 2026-07-28 but refused, with the list, at a revision the server lacks.
func TestServerNegotiatesItsSupportedProtocols(t *testing.T) {
	root := t.TempDir()
	if got := connectMCPClient(t, root).InitializeResult().ProtocolVersion; got != machinecontract.MCPProtocolCurrent {
		t.Fatalf("an SDK client negotiated %q, want %q", got, machinecontract.MCPProtocolCurrent)
	}
	server, err := NewServer(root, "protocol-versions-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	pinned, err := mcp.NewClient(&mcp.Implementation{Name: "pinned", Version: "1"}, nil).Connect(context.Background(), clientTransport,
		&mcp.ClientSessionOptions{ProtocolVersion: machinecontract.MCPProtocolHandshakeNewest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pinned.Close() })
	if got := pinned.InitializeResult().ProtocolVersion; got != machinecontract.MCPProtocolHandshakeNewest {
		t.Fatalf("a pinned client negotiated %q", got)
	}

	raw := newRawMCP(t, root)
	discovered, wire := raw.call("server/discover", map[string]any{"_meta": sessionlessMeta(machinecontract.MCPProtocolSessionless, map[string]any{})})
	if wire != nil {
		t.Fatalf("a sessionless discover was refused: %v", wire)
	}
	supported, _ := json.Marshal(discovered["supportedVersions"])
	listed, _ := json.Marshal(machinecontract.MCPSupportedProtocolVersions())
	if string(supported) != string(listed) {
		t.Fatalf("discover advertised %s, want %s", supported, listed)
	}

	handshake := newRawMCP(t, root)
	initialized, wire := handshake.call("initialize", map[string]any{"protocolVersion": machinecontract.MCPProtocolSessionless,
		"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "raw-mcp-test", "version": "1"}})
	if wire != nil || initialized["protocolVersion"] != machinecontract.MCPProtocolHandshakeNewest {
		t.Fatalf("initialize offering %s answered %v %v", machinecontract.MCPProtocolSessionless, initialized["protocolVersion"], wire)
	}

	future := newRawMCP(t, root)
	if _, wire := future.call("server/discover", map[string]any{"_meta": sessionlessMeta("2099-01-01", map[string]any{})}); wire == nil ||
		wire.Code != mcp.CodeUnsupportedProtocolVersion {
		t.Fatalf("a revision the server lacks was not refused as unsupported: %v", wire)
	} else {
		var data mcp.UnsupportedProtocolVersionData
		if err := json.Unmarshal(wire.Data, &data); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(data.Supported, machinecontract.MCPSupportedProtocolVersions()) {
			t.Fatalf("refusal advertised %v", data.Supported)
		}
	}
}
