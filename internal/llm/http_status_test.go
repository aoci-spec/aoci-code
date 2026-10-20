package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type statusResponseTransport struct {
	status int
	body   io.ReadCloser
}

func (transport statusResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: transport.status, Body: transport.body, Header: make(http.Header)}, nil
}

type unreadableStatusBody struct {
	reads  int
	closed bool
}

func (body *unreadableStatusBody) Read([]byte) (int, error) {
	body.reads++
	return 0, io.ErrUnexpectedEOF
}

func (body *unreadableStatusBody) Close() error {
	body.closed = true
	return nil
}

func TestCompleteClassifiesHTTPFailureWithoutReadingBody(t *testing.T) {
	for _, tc := range []struct {
		status    int
		kind      Kind
		retryable bool
	}{
		{http.StatusFound, KindHTTP, false},
		{http.StatusBadRequest, KindHTTP, false},
		{http.StatusUnauthorized, KindAuth, false},
		{http.StatusForbidden, KindAuth, false},
		{http.StatusTooManyRequests, KindRateLimit, true},
		{http.StatusServiceUnavailable, KindServer, true},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			body := &unreadableStatusBody{}
			client, err := NewClient(Options{
				BaseURL: "https://example.test", Model: "test",
				HTTPClient: &http.Client{Transport: statusResponseTransport{status: tc.status, body: body}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Complete(context.Background(), CompletionRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
			})
			var failure *Error
			if result != nil || !errors.As(err, &failure) {
				t.Fatalf("expected HTTP failure, got result=%v error=%v", result, err)
			}
			if failure.Kind != tc.kind || failure.StatusCode != tc.status || failure.IsRetryable() != tc.retryable {
				t.Errorf("HTTP classification lost: kind=%v status=%d retryable=%t", failure.Kind, failure.StatusCode, failure.IsRetryable())
			}
			if failure.Err != nil {
				t.Errorf("HTTP failure retained body error: %v", failure.Err)
			}
			if body.reads != 0 || !body.closed {
				t.Errorf("error body reads=%d closed=%t; want unread and closed", body.reads, body.closed)
			}
		})
	}
}
