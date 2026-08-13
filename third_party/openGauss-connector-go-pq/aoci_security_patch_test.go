package pq

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseConfigStrictIgnoresAmbientConfiguration(t *testing.T) {
	ambient := map[string]string{
		"HOME":                 t.TempDir(),
		"PGHOST":               "ambient.invalid",
		"PGPORT":               "1",
		"PGDATABASE":           "ambient_database",
		"PGUSER":               "ambient_user",
		"PGPASSWORD":           "ambient_password",
		"PGPASSFILE":           filepath.Join(t.TempDir(), "ambient.pgpass"),
		"PGAPPNAME":            "ambient_application",
		"PGCONNECT_TIMEOUT":    "999",
		"PGSSLMODE":            "disable",
		"PGSSLKEY":             filepath.Join(t.TempDir(), "ambient.key"),
		"PGSSLCERT":            filepath.Join(t.TempDir(), "ambient.crt"),
		"PGSSLROOTCERT":        filepath.Join(t.TempDir(), "ambient-root.crt"),
		"PGTARGETSESSIONATTRS": "read-only",
		"PGSERVICE":            "ambient_service",
		"PGSERVICEFILE":        filepath.Join(t.TempDir(), "ambient-service.conf"),
		"PGLOGGERLEVEL":        "trace",
	}
	for name, value := range ambient {
		setenvForTest(t, name, value)
	}

	config, err := ParseConfigStrict("host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=verify-full")
	if err != nil {
		t.Fatalf("ParseConfigStrict returned an error: %v", err)
	}
	if config.Host != "db.example.test" || config.Port != 5432 || config.Database != "app" || config.User != "reader" || config.Password != "secret" {
		t.Fatalf("strict config used ambient connection settings: %#v", config)
	}
	if config.ConnectTimeout != 0 {
		t.Fatalf("strict config used ambient connect timeout: %v", config.ConnectTimeout)
	}
	if config.Logger != nil || config.LogLevel != 0 {
		t.Fatalf("strict config used ambient logger settings: logger=%T level=%v", config.Logger, config.LogLevel)
	}
	if got := config.RuntimeParams[paramApplicationName]; got != "" {
		t.Fatalf("strict config used ambient application name: %q", got)
	}
	if config.TLSConfig == nil || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.ServerName != "db.example.test" {
		t.Fatalf("strict config did not preserve verify-full: %#v", config.TLSConfig)
	}
	if len(config.Fallbacks) != 0 {
		t.Fatalf("strict config created fallback connections: %#v", config.Fallbacks)
	}
}

func TestParseConfigStrictRequiresExplicitConnectionFields(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"host", "port=5432 dbname=app user=reader password=secret sslmode=disable", "host must be explicitly provided"},
		{"port", "host=localhost dbname=app user=reader password=secret sslmode=disable", "port must be explicitly provided"},
		{"database", "host=localhost port=5432 user=reader password=secret sslmode=disable", "database must be explicitly provided"},
		{"user", "host=localhost port=5432 dbname=app password=secret sslmode=disable", "user must be explicitly provided"},
		{"password", "host=localhost port=5432 dbname=app user=reader sslmode=disable", "password must be explicitly provided"},
		{"sslmode", "host=localhost port=5432 dbname=app user=reader password=secret", "sslmode must be explicitly provided"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseConfigStrict(test.dsn)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseConfigStrict error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseConfigStrictRejectsAmbientFileAndLoggerFeatures(t *testing.T) {
	for _, parameter := range []string{
		paramService, paramServiceFile, paramPassFile, paramLoggerLevel,
		paramConnectTimeout, paramTargetSessionAttrs, paramKrbSrvName,
		paramClientEncoding, paramMinReadBufferSize, "search_path",
	} {
		t.Run(parameter, func(t *testing.T) {
			dsn := "host=localhost port=5432 dbname=app user=reader password=secret sslmode=disable " + parameter + "=forbidden"
			_, err := ParseConfigStrict(dsn)
			if err == nil || !strings.Contains(err.Error(), parameter+" is not permitted") {
				t.Fatalf("ParseConfigStrict error = %v, want forbidden %s", err, parameter)
			}
		})
	}
}

func TestParseConfigStrictTLSPolicy(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr string
		wantTLS bool
	}{
		{"remote verify-full", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=verify-full", "", true},
		{"localhost disable", "host=localhost port=5432 dbname=app user=reader password=secret sslmode=disable", "numeric loopback", false},
		{"IPv4 loopback disable", "host=127.0.0.1 port=5432 dbname=app user=reader password=secret sslmode=disable", "", false},
		{"IPv6 loopback disable", "host=::1 port=5432 dbname=app user=reader password=secret sslmode=disable", "", false},
		{"Unix socket disable", "host=/tmp port=5432 dbname=app user=reader password=secret sslmode=disable", "", false},
		{"remote disable", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=disable", "numeric loopback", false},
		{"allow", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=allow", "not permitted", false},
		{"prefer", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=prefer", "not permitted", false},
		{"require", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=require", "not permitted", false},
		{"verify-ca", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=verify-ca", "not permitted", false},
		{"Unix socket verify-full", "host=/tmp port=5432 dbname=app user=reader password=secret sslmode=verify-full", "must use sslmode=disable", false},
		{"multiple hosts", "host=localhost,127.0.0.1 port=5432 dbname=app user=reader password=secret sslmode=disable", "multiple hosts", false},
		{"multiple ports", "host=localhost port=5432,5433 dbname=app user=reader password=secret sslmode=disable", "multiple hosts", false},
		{"MogDB URL", "mogdb://reader:secret@127.0.0.1:5432/app?sslmode=disable", "protocol mogdb", false},
		{"unknown URL", "https://reader:secret@127.0.0.1:5432/app?sslmode=disable", "protocol https", false},
		{"relative CA", "host=db.example.test port=5432 dbname=app user=reader password=secret sslmode=verify-full sslrootcert=relative-ca.pem", "absolute path", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := ParseConfigStrict(test.dsn)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ParseConfigStrict error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConfigStrict returned an error: %v", err)
			}
			if got := config.TLSConfig != nil; got != test.wantTLS {
				t.Fatalf("TLSConfig presence = %v, want %v", got, test.wantTLS)
			}
			if config.TLSConfig != nil && config.TLSConfig.MinVersion != tls.VersionTLS12 {
				t.Fatalf("strict TLS minimum version = %d, want TLS 1.2", config.TLSConfig.MinVersion)
			}
			if len(config.Fallbacks) != 0 {
				t.Fatalf("strict config created fallback connections: %#v", config.Fallbacks)
			}
		})
	}
}

func TestConnectorContextBoundsTLSStartup(t *testing.T) {
	config, err := ParseConfigStrict("host=127.0.0.1 port=5432 dbname=app user=reader password=secret sslmode=verify-full")
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) { return client, nil }
	connector, err := NewConnectorConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	connection, err := connector.Connect(ctx)
	if err == nil {
		if connection != nil {
			_ = connection.Close()
		}
		t.Fatal("TLS blackhole unexpectedly produced a connection")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("TLS blackhole error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("TLS blackhole ignored context for %v", elapsed)
	}
}

func TestNewPrintfLoggerWritesToStderr(t *testing.T) {
	logger, ok := NewPrintfLogger(LogLevelInfo).(*printfLogger)
	if !ok {
		t.Fatalf("NewPrintfLogger returned %T", logger)
	}
	if logger.l.Writer() != os.Stderr {
		t.Fatalf("NewPrintfLogger writer = %T, want os.Stderr", logger.l.Writer())
	}
}

func TestBadClientCertificateDoesNotWriteStdout(t *testing.T) {
	tempDir := t.TempDir()
	certBytes, err := os.ReadFile(filepath.Join("certs", "server.crt"))
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := os.ReadFile(filepath.Join("certs", "postgresql.key"))
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(tempDir, "client.crt")
	keyPath := filepath.Join(tempDir, "client.key")
	if err := os.WriteFile(certPath, certBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyBytes, 0600); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		_, err = configTLSStrict(map[string]string{
			paramHost:    "db.example.test",
			paramSSLMode: "verify-full",
			paramSSLCert: certPath,
			paramSSLKey:  keyPath,
		})
	})
	if err == nil {
		t.Fatal("configTLSStrict accepted a mismatched client certificate and key")
	}
	if output != "" {
		t.Fatalf("configTLSStrict wrote to stdout: %q", output)
	}
}

func TestCancelTLSFailureUsesOnlyCancelConnection(t *testing.T) {
	mainConnection := &recordingConn{}
	cancelConnection := &recordingConn{writeErr: errors.New("cancel TLS write failed")}
	cn := &conn{
		c: mainConnection,
		config: &Config{
			DialFunc: func(context.Context, string, string) (net.Conn, error) {
				return cancelConnection, nil
			},
			minReadBufferSize: 8,
		},
		fallbackConfig: &FallbackConfig{
			Host:      "localhost",
			Port:      5432,
			TLSConfig: &tls.Config{},
		},
	}

	err := cn.cancel(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cancel TLS write failed") {
		t.Fatalf("cancel error = %v, want cancel TLS failure", err)
	}
	if mainConnection.writes != 0 || mainConnection.closed {
		t.Fatalf("cancel touched main connection: writes=%d closed=%v", mainConnection.writes, mainConnection.closed)
	}
	if cancelConnection.writes == 0 || !cancelConnection.closed {
		t.Fatalf("cancel connection was not used and closed: writes=%d closed=%v", cancelConnection.writes, cancelConnection.closed)
	}
}

func TestCancelContextBoundsTLSStartupAndServerEOF(t *testing.T) {
	for _, test := range []struct {
		name      string
		tlsConfig *tls.Config
		drain     bool
	}{
		{name: "TLS response stall", tlsConfig: &tls.Config{}},
		{name: "server EOF stall", drain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()
			if test.drain {
				go func() { _, _ = io.Copy(io.Discard, server) }()
			}
			t.Cleanup(func() { _ = server.Close() })
			cn := &conn{
				config: &Config{
					DialFunc:          func(context.Context, string, string) (net.Conn, error) { return client, nil },
					minReadBufferSize: 8,
				},
				fallbackConfig: &FallbackConfig{Host: "127.0.0.1", Port: 5432, TLSConfig: test.tlsConfig},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			started := time.Now()
			err := cn.cancel(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancel stall error = %v, want context deadline", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("cancel stall ignored context for %v", elapsed)
			}
		})
	}
}

func TestServerPBKDF2IterationBounds(t *testing.T) {
	for _, iterations := range []int{1, maxServerPBKDF2Iterations} {
		if err := validateServerPBKDF2Iterations(iterations); err != nil {
			t.Fatalf("validateServerPBKDF2Iterations(%d) = %v", iterations, err)
		}
	}
	for _, iterations := range []int{-1, 0, maxServerPBKDF2Iterations + 1} {
		if err := validateServerPBKDF2Iterations(iterations); err == nil {
			t.Fatalf("validateServerPBKDF2Iterations(%d) unexpectedly succeeded", iterations)
		}
	}
}

func TestAuthenticationPayloadAndIterationFailuresAreDriverErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{"short SHA256 method", authPayload(AuthReqSha256, nil), "invalid SHA256 authentication method payload"},
		{"short SHA256 challenge", authPayload(AuthReqSha256, int32Bytes(Sha256Password)), "invalid SHA256 authentication challenge payload"},
		{"zero SHA256 iterations", challengePayload(AuthReqSha256, Sha256Password, 0), "PBKDF2 iteration count 0"},
		{"excessive SHA256 iterations", challengePayload(AuthReqSha256, Sha256Password, maxServerPBKDF2Iterations+1), "outside the permitted range"},
		{"short SM3 method", authPayload(AuthReqSm3, nil), "invalid SM3 authentication method payload"},
		{"short SM3 challenge", authPayload(AuthReqSm3, int32Bytes(Sm3Password)), "invalid SM3 authentication challenge payload"},
		{"zero SM3 iterations", challengePayload(AuthReqSm3, Sm3Password, 0), "PBKDF2 iteration count 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cn := &conn{config: &Config{Password: "secret"}}
			buf := readBuf(test.payload)
			got := recoverAuthPanic(func() { cn.auth(&buf) })
			if got == nil {
				t.Fatal("auth unexpectedly succeeded")
			}
			if _, ok := got.(runtime.Error); ok {
				t.Fatalf("auth panicked with runtime error: %v", got)
			}
			err, ok := got.(error)
			if !ok || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("auth panic = %#v, want driver error containing %q", got, test.want)
			}
		})
	}
}

func authPayload(code int, payload []byte) []byte {
	result := int32Bytes(code)
	return append(result, payload...)
}

func challengePayload(code, method, iterations int) []byte {
	payload := int32Bytes(method)
	payload = append(payload, make([]byte, 64+8)...)
	payload = append(payload, int32Bytes(iterations)...)
	return authPayload(code, payload)
}

func int32Bytes(value int) []byte {
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, uint32(value))
	return result
}

func recoverAuthPanic(fn func()) (recovered interface{}) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func setenvForTest(t *testing.T, name, value string) {
	t.Helper()
	original, present := os.LookupEnv(name)
	if err := os.Setenv(name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, original)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

type recordingConn struct {
	writes   int
	closed   bool
	writeErr error
}

func (c *recordingConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *recordingConn) Write(p []byte) (int, error)      { c.writes++; return len(p), c.writeErr }
func (c *recordingConn) Close() error                     { c.closed = true; return nil }
func (c *recordingConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *recordingConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *recordingConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }
