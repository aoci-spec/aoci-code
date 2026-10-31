// Bounded stdin reading: the single upper-bound entry of CLI input
// Index entry: stdin_limit.go[CG7T]
//
// Motivation: hook.go and update_entry.go both read without bound through io.ReadAll(os.Stdin). The rest of the repository
// was bounded long ago: index_agent_stage_protocol.go and index_agent_curation_protocol.go
// both go through LimitReader(max+1) and compare the length. These two CLI channels missed the same discipline.
//
// max+1 is deliberate: only reading exactly max+1 bytes tells "exactly at the limit" from "over the limit";
// one byte less and the two cannot be told apart.
package cli

import "io"

// hookInputMaxBytes is deliberately small: the hook JSON carries only the tool name and the path, and the hook infrastructure
// must fail open, never allocating a buffer for an untrusted editor payload.
const hookInputMaxBytes int64 = 64 << 10

// readLimitedInput reads at most maxBytes bytes. oversize true means the input exceeded the limit,
// in which case no content is returned; each caller decides whether to pass or to error.
func readLimitedInput(reader io.Reader, maxBytes int64) (data []byte, oversize bool, err error) {
	data, err = io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, true, nil
	}
	return data, false, nil
}
