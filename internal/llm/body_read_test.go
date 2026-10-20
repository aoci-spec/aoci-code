package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type bodyFailureTransport struct{ cause error }

func (transport bodyFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bodyFailureReader{transport.cause})}, nil
}

type bodyFailureReader struct{ cause error }

func (reader bodyFailureReader) Read([]byte) (int, error) { return 0, reader.cause }

func TestCompleteBodyReadErrorsKeepClassification(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		kind  Kind
	}{
		{"deadline", fmt.Errorf("private-url?token=synthetic-key: %w", context.DeadlineExceeded), KindTimeout},
		{"network timeout", fmt.Errorf("private-source: %w", timeoutErr{}), KindTimeout},
		{"cancelled", context.Canceled, KindNetwork},
		{"truncated", io.ErrUnexpectedEOF, KindNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(Options{BaseURL: "https://example.test", Model: "test",
				HTTPClient: &http.Client{Transport: bodyFailureTransport{tc.cause}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "test"}}})
			var failure *Error
			if !errors.As(err, &failure) || failure.Kind != tc.kind || failure.StatusCode != http.StatusOK {
				t.Fatalf("lost body-read classification: %v", err)
			}
			if !errors.Is(err, tc.cause) || failure.IsRetryable() != (tc.kind == KindTimeout) {
				t.Fatalf("lost cause or retryability: %v", err)
			}
			for _, forbidden := range []string{"synthetic-key", "private-url", "private-source", "example.test"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("body-read error exposed %q", forbidden)
				}
			}
		})
	}
}
