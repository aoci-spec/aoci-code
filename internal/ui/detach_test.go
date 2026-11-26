package ui

import (
	"strings"
	"testing"
)

func TestDetachStartupOutputIsBounded(t *testing.T) {
	for _, prefix := range []string{"", "{\n"} {
		stdout := strings.NewReader(prefix + strings.Repeat("x", detachStartupMaxBytes*2))
		_, err := readDetachStartup(stdout)
		if err == nil || !strings.Contains(err.Error(), "exceeds") || stdout.Len() != len(prefix)+detachStartupMaxBytes-1 {
			t.Fatalf("startup read must stop one byte beyond the limit: remaining=%d err=%v", stdout.Len(), err)
		}
	}
}

func TestDetachStartupAcceptsReadinessWithoutWaitingForEOF(t *testing.T) {
	stdout := strings.NewReader("{\"url\":\"http://127.0.0.1:1234/\",\"repositories\":2}\n" + strings.Repeat("x", detachStartupMaxBytes))
	ready, err := readDetachStartup(stdout)
	if err != nil || ready.URL != "http://127.0.0.1:1234/" || ready.Repositories != 2 || stdout.Len() == 0 {
		t.Fatalf("expected readiness from a running child: %+v err=%v", ready, err)
	}
}
