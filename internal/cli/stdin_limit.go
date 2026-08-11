package cli

import "io"

// hookInputMaxBytes is intentionally small: hook JSON carries only a tool name
// and path, and hook infrastructure must fail open instead of buffering an
// untrusted editor payload.
const hookInputMaxBytes int64 = 64 << 10

func readLimitedInput(reader io.Reader, maxBytes int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, true, nil
	}
	return data, false, nil
}
