package cli

import (
	"bytes"
	"errors"
	"testing"
)

type failingInputReader struct{}

func (failingInputReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestReadLimitedInputRejectsOversizeWithoutUnboundedRead(t *testing.T) {
	const limit = int64(32)
	exact := bytes.Repeat([]byte("x"), int(limit))
	data, tooLarge, err := readLimitedInput(bytes.NewReader(exact), limit)
	if err != nil || tooLarge || !bytes.Equal(data, exact) {
		t.Fatalf("exact input rejected: len=%d too_large=%v err=%v", len(data), tooLarge, err)
	}
	data, tooLarge, err = readLimitedInput(bytes.NewReader(append(exact, 'x')), limit)
	if err != nil || !tooLarge || data != nil {
		t.Fatalf("oversize input accepted: len=%d too_large=%v err=%v", len(data), tooLarge, err)
	}
	if _, _, err := readLimitedInput(failingInputReader{}, limit); err == nil {
		t.Fatal("reader failure was discarded")
	}
}
