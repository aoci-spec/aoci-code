// File line counting (the line-count fact source of the E-scale gate)
// Index entry pending: countlines.go
//
// Line semantics match the inventory profile: a last line without a newline still counts as one; an empty file has 0 lines.
// The count streams instead of reading the file into memory (safe for any size); deliberately no binary sniffing: the caller
// (the E-scale gate) produces at most one harmless warning for a binary entry, not worth a new copy of sniffing
// (sniffing already exists twice, in indexgen and workflow; merging them belongs to the P2 hygiene ledger, no third copy here).
package fs

import (
	"bytes"
	"io"
	"os"
)

// CountFileLines counts a file's lines as a stream.
// Semantics: an empty file returns 0; with a trailing newline the count is the number of newlines; a last line without one adds 1.
func CountFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return CountLines(f)
}

// CountLines counts a stream with the same final-line rules as CountFileLines.
// Callers may tee the stream to bind the count to its exact source digest.
func CountLines(reader io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lastByte := byte('\n') // sentinel: an empty file does not trigger the last-line +1
	empty := true
	for {
		n, rerr := reader.Read(buf)
		if n > 0 {
			empty = false
			count += bytes.Count(buf[:n], []byte{'\n'})
			lastByte = buf[n-1]
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, rerr
		}
	}
	if empty {
		return 0, nil
	}
	if lastByte != '\n' {
		count++
	}
	return count, nil
}
