// File fingerprint computation.
//
// The raw SHA-256 always covers the complete raw bytes.
// For text files meeting the conditions, the normalized fingerprint that only folds CRLF to LF is computed as well.
//
// Safety boundary:
//   - no handling of a BOM, a lone CR, a trailing newline, or any other byte difference;
//   - a NUL within the first 8000 bytes means binary, and no normalized fingerprint is produced;
//   - above 4 MiB only the raw fingerprint is produced; the degradation is always toward stricter;
//   - the normalized fingerprint is never used for CAS or the Stage source binding.
//   - FormatSHA256 is computed only for Go source that parses completely, and no loose heuristic such as whitespace stripping
//     replaces the formatter, so a string, comment, or token change never enters the format-only path.
package baseline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/format"
	"hash"
	"io"
	"os"
	"path/filepath"
)

const (
	// normalizedFingerprintMaxBytes caps the file size for normalized computation.
	normalizedFingerprintMaxBytes int64 = 4 << 20

	// normalizedFingerprintBinarySniffBytes stays consistent with the Curation binary profile window.
	//
	// The baseline package cannot depend on the curation package in reverse, so the same-valued constant is kept here;
	// a later change to either side must update the other and its tests.
	normalizedFingerprintBinarySniffBytes int64 = 8000

	fingerprintReadBufferBytes = 32 * 1024
)

// HashFile streams the file's raw SHA-256, real byte count, and optional normalized fingerprint.
func HashFile(path string) (Fingerprint, error) {
	return hashFile(path, nil)
}

// HashFileReusing returns exactly what HashFile returns, but when the raw digest
// and size match a Fingerprint the caller already holds it reuses that
// Fingerprint's formatted digest instead of reparsing the source.
//
// The reuse is output-identical by construction: FormatSHA256 is a deterministic
// function of the bytes, and identical bytes cannot produce a different one. The
// point is cost, not semantics. HashFile runs go/format.Source on every Go file,
// which profiles at 94.7% of HashFile against 1.5% for the SHA-256 it supplements
// — the formatter is roughly 63x the hash it accompanies — and IsFormatOnlyChange
// requires before.SHA256 != after.SHA256 as its first condition, so for a file
// whose raw digest is unchanged that computation can never be consulted.
//
// prior is untrusted input: Baseline.Load performs no per-Fingerprint validation,
// so a hand-edited or truncated baseline.json can carry a format_kind with no
// format_sha256. Reusing that pair would install a state HashFile itself can
// never produce, so both fields must be present before either is carried over,
// and prior must agree with all three digests this call computes from the file
// itself before any of its own values are trusted.
//
// This is not an integrity boundary and does not weaken one. SHA256 is the
// integrity anchor and is always computed from the bytes on every call.
// FormatSHA256 is a derived digest whose only judgement, IsFormatOnlyChange,
// requires the raw digest to have CHANGED — which is exactly the case where no
// reuse happens and the value is recomputed.
//
// One residual is accepted knowingly and is pinned by
// TestHashFileReusingCarriesATamperedDigestOnlyForUnparseableGoSource: a .go file
// that does not parse produces no formatted digest here, yet a prior claiming one
// for those exact bytes would be carried over. HashFile cannot produce that prior
// — identical bytes always parse identically — so it is reachable only by editing
// baseline.json by hand, which is CAS-protected and separately verified. Closing
// it costs a go/parser run on every Go file on every call, which is the majority
// of the cost this function exists to avoid.
func HashFileReusing(path string, prior Fingerprint) (Fingerprint, error) {
	return hashFile(path, &prior)
}

func hashFile(path string, reuse *Fingerprint) (Fingerprint, error) {
	file, err := os.Open(path)
	if err != nil {
		return Fingerprint{}, err
	}
	defer file.Close()

	rawHash := sha256.New()
	normalizedHash := sha256.New()

	buffer := make(
		[]byte,
		fingerprintReadBufferBytes,
	)

	var totalBytes int64
	var sniffedBytes int64

	normalizedEligible := true
	binaryDetected := false
	pendingCR := false
	collectGoSource := filepath.Ext(path) == ".go"
	goSource := []byte{}

	for {
		readCount, readErr := file.Read(buffer)

		if readCount > 0 {
			chunk := buffer[:readCount]
			if collectGoSource {
				if totalBytes+int64(readCount) > normalizedFingerprintMaxBytes {
					collectGoSource = false
					goSource = nil
				} else {
					goSource = append(goSource, chunk...)
				}
			}

			if _, err := rawHash.Write(chunk); err != nil {
				return Fingerprint{}, err
			}

			totalBytes += int64(readCount)

			if sniffedBytes < normalizedFingerprintBinarySniffBytes {
				remaining := normalizedFingerprintBinarySniffBytes -
					sniffedBytes

				sniffCount := readCount
				if int64(sniffCount) > remaining {
					sniffCount = int(remaining)
				}

				if bytes.IndexByte(
					chunk[:sniffCount],
					0,
				) >= 0 {
					binaryDetected = true
				}

				sniffedBytes += int64(sniffCount)
			}

			if normalizedEligible {
				if totalBytes > normalizedFingerprintMaxBytes {
					// Past the ceiling, the normalized intermediate state so far is discarded.
					// The raw hash keeps covering the complete file.
					normalizedEligible = false
					pendingCR = false
				} else {
					pendingCR = foldCRLFInto(
						normalizedHash,
						chunk,
						pendingCR,
					)
				}
			}
		}

		if readErr == io.EOF {
			break
		}

		if readErr != nil {
			return Fingerprint{}, readErr
		}
	}

	if normalizedEligible && pendingCR {
		if _, err := normalizedHash.Write(
			[]byte{'\r'},
		); err != nil {
			return Fingerprint{}, err
		}
	}

	result := Fingerprint{
		SHA256: hex.EncodeToString(
			rawHash.Sum(nil),
		),
		Size: totalBytes,
	}

	if normalizedEligible && !binaryDetected {
		result.NormalizedSHA256 = hex.EncodeToString(
			normalizedHash.Sum(nil),
		)
	}

	if collectGoSource && !binaryDetected && reuse != nil &&
		reuse.SHA256 == result.SHA256 && reuse.Size == result.Size &&
		reuse.NormalizedSHA256 != "" && reuse.NormalizedSHA256 == result.NormalizedSHA256 &&
		reuse.FormatKind == "gofmt" && reuse.FormatSHA256 != "" {
		result.FormatSHA256 = reuse.FormatSHA256
		result.FormatKind = reuse.FormatKind
		collectGoSource = false
	}
	if collectGoSource && !binaryDetected {
		formatted, formatErr := format.Source(goSource)
		if formatErr == nil {
			digest := sha256.Sum256(formatted)
			result.FormatSHA256 = hex.EncodeToString(digest[:])
			result.FormatKind = "gofmt"
		}
	}

	return result, nil
}

// HashBytes computes the same Baseline fingerprint as HashFile for bytes that
// do not yet have an on-disk formal path. The logical path is used only to
// select the supported source formatter; no file is created.
func HashBytes(logicalPath string, data []byte) Fingerprint {
	rawDigest := sha256.Sum256(data)
	result := Fingerprint{
		SHA256: hex.EncodeToString(rawDigest[:]),
		Size:   int64(len(data)),
	}
	if int64(len(data)) <= normalizedFingerprintMaxBytes {
		sniff := data
		if len(sniff) > int(normalizedFingerprintBinarySniffBytes) {
			sniff = sniff[:normalizedFingerprintBinarySniffBytes]
		}
		if bytes.IndexByte(sniff, 0) < 0 {
			normalized := bytes.ReplaceAll(data, []byte{'\r', '\n'}, []byte{'\n'})
			normalizedDigest := sha256.Sum256(normalized)
			result.NormalizedSHA256 = hex.EncodeToString(normalizedDigest[:])
			if filepath.Ext(logicalPath) == ".go" {
				if formatted, err := format.Source(data); err == nil {
					formattedDigest := sha256.Sum256(formatted)
					result.FormatSHA256 = hex.EncodeToString(formattedDigest[:])
					result.FormatKind = "gofmt"
				}
			}
		}
	}
	return result
}

// IsFormatOnlyChange accepts only canonical digests from the same supported formatter that agree exactly.
// The raw digests must differ, so an unchanged file is not counted into the fast path.
func IsFormatOnlyChange(before, after Fingerprint) bool {
	return before.SHA256 != after.SHA256 &&
		before.FormatKind != "" &&
		before.FormatKind == after.FormatKind &&
		before.FormatSHA256 != "" &&
		before.FormatSHA256 == after.FormatSHA256
}

// foldCRLFInto folds the CRLFs of chunk into LF and writes into dst.
//
// pendingCR means the previous chunk ended with a CR; the return value means the current chunk still ends with a pending CR.
// A lone CR keeps its raw byte, so a real content change is never mistaken for a line-ending representation change.
func foldCRLFInto(
	dst hash.Hash,
	chunk []byte,
	pendingCR bool,
) bool {
	if len(chunk) == 0 {
		return pendingCR
	}

	start := 0

	if pendingCR {
		if chunk[0] == '\n' {
			_, _ = dst.Write(
				[]byte{'\n'},
			)
			start = 1
		} else {
			_, _ = dst.Write(
				[]byte{'\r'},
			)
		}
	}

	output := make(
		[]byte,
		0,
		len(chunk)-start,
	)

	for index := start; index < len(chunk); index++ {
		current := chunk[index]

		if current != '\r' {
			output = append(
				output,
				current,
			)
			continue
		}

		if index+1 >= len(chunk) {
			if len(output) > 0 {
				_, _ = dst.Write(output)
			}
			return true
		}

		if chunk[index+1] == '\n' {
			output = append(
				output,
				'\n',
			)
			index++
			continue
		}

		output = append(
			output,
			'\r',
		)
	}

	if len(output) > 0 {
		_, _ = dst.Write(output)
	}

	return false
}
