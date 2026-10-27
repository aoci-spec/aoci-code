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

// The SDK prefers the 2026-07-28 revision, which drops the initialize
// handshake and blocks the server-initiated elicitation human_approval uses.
// An SDK client that would prefer it must land on MCPProtocolCurrent through
// the handshake, and a sessionless request at that revision must be refused
// with the supported list rather than served.
func TestServerNegotiatesOnlyHandshakeEraProtocols(t *testing.T) {
	root := t.TempDir()
	session := connectMCPClient(t, root)
	if got := session.InitializeResult().ProtocolVersion; got != machinecontract.MCPProtocolCurrent {
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
	conn, err := clientTransport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	request, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"protocol-versions-test","version":"1"},` +
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	message, err := conn.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response, ok := message.(*jsonrpc.Response)
	if !ok {
		t.Fatalf("expected a response, got %T", message)
	}
	var wire *jsonrpc.Error
	if !errors.As(response.Error, &wire) || wire.Code != mcp.CodeUnsupportedProtocolVersion {
		t.Fatalf("a 2026-07-28 request was not refused as unsupported: result=%s error=%v", response.Result, response.Error)
	}
	var data mcp.UnsupportedProtocolVersionData
	if err := json.Unmarshal(wire.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(data.Supported, machinecontract.MCPSupportedProtocolVersions()) || data.Requested != "2026-07-28" {
		t.Fatalf("refusal advertised %v for %q, want %v", data.Supported, data.Requested, machinecontract.MCPSupportedProtocolVersions())
	}
}
