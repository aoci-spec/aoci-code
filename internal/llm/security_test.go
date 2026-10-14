package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompleteNeverFollowsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/custom=%t", status, custom), func(t *testing.T) {
				var destinationCalls, callbackCalls atomic.Int32
				destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					destinationCalls.Add(1)
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"unexpected"}}]}`)
				}))
				defer destination.Close()
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, destination.URL, status)
				}))
				defer origin.Close()
				opts := Options{BaseURL: origin.URL, Model: "test", APIKey: "synthetic-key"}
				if custom {
					opts.HTTPClient = origin.Client()
					opts.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error { callbackCalls.Add(1); return nil }
				}
				client, err := NewClient(opts)
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "synthetic source"}}})
				var failure *Error
				if !errors.As(err, &failure) || failure.StatusCode != status || failure.IsRetryable() {
					t.Fatalf("redirect was not refused: %v", err)
				}
				if destinationCalls.Load() != 0 || callbackCalls.Load() != 0 {
					t.Fatal("redirect escaped the configured endpoint")
				}
				if custom {
					_ = opts.HTTPClient.CheckRedirect(nil, nil)
					if callbackCalls.Load() != 1 {
						t.Fatal("caller-owned client was mutated")
					}
				}
			})
		}
	}
}

func TestCompleteErrorsDoNotRetainEndpointPayloads(t *testing.T) {
	for _, status := range []int{200, 401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"synthetic-key private-source\nforged-log-line"}}`)
			}))
			defer endpoint.Close()
			client := newTestClient(t, endpoint, "synthetic-key")
			_, err := client.Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "private-source"}}})
			if err == nil {
				t.Fatal("error response accepted")
			}
			for _, forbidden := range []string{"synthetic-key", "private-source", "forged-log-line", endpoint.URL} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatal("untrusted endpoint detail escaped")
				}
			}
		})
	}
}

type failingTransport struct{ err error }

func (transport failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

func TestTransportFailureKeepsClassificationWithoutFormattingCause(t *testing.T) {
	cause := fmt.Errorf("private-url?token=synthetic-key: %w", context.DeadlineExceeded)
	client, err := NewClient(Options{BaseURL: "https://example.test", Model: "test", HTTPClient: &http.Client{Transport: failingTransport{cause}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "test"}}})
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != KindTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost timeout classification: %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-key") || strings.Contains(err.Error(), "example.test") {
		t.Fatal("transport detail escaped")
	}
}

func TestClientRefusesCredentialBearingOrAmbiguousEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://user:synthetic-key@example.test", "https://example.test?token=synthetic-key", "https://example.test#synthetic-key", "file:///private", "https:///missing-host", "://synthetic-key"} {
		_, err := NewClient(Options{BaseURL: endpoint, Model: "test"})
		if err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
		if strings.Contains(err.Error(), endpoint) || strings.Contains(err.Error(), "synthetic-key") {
			t.Fatal("invalid endpoint echoed")
		}
	}
}
