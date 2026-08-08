// 文件指纹计算。
//
// 原始SHA-256始终覆盖完整原始字节。
// 对满足条件的文本文件，同时计算只把CRLF折叠为LF的规范化指纹。
//
// 安全边界:
//   - 不处理BOM、孤立CR、尾换行或任何其他字节差异;
//   - 前8000字节出现NUL时按二进制处理，不生成规范化指纹;
//   - 超过4MiB时只生成原始指纹，退化方向始终是更严格;
//   - 规范化指纹绝不用于CAS或Stage源码绑定。
//   - FormatSHA256只对可完整解析的Go源码计算，不以空白剥离等宽松启发式
//     代替格式器，因此字符串、注释或token变化不会进入format-only路径。
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
	// normalizedFingerprintMaxBytes限制规范化计算的最大文件大小。
	normalizedFingerprintMaxBytes int64 = 4 << 20

	// normalizedFingerprintBinarySniffBytes与Curation二进制画像窗口保持一致。
	//
	// baseline包不能反向依赖curation包，因此在此保留同值常量；
	// 后续修改任一侧时必须同步另一侧并更新测试。
	normalizedFingerprintBinarySniffBytes int64 = 8000

	fingerprintReadBufferBytes = 32 * 1024
)

// HashFile流式计算文件原始SHA-256、实际字节数和可选规范化指纹。
func HashFile(path string) (Fingerprint, error) {
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
					// 已经越过上限，丢弃此前的规范化中间态。
					// 原始哈希继续覆盖完整文件。
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

// IsFormatOnlyChange只承认同一受支持格式器生成的规范摘要完全相同。
// 原始摘要必须不同，防止无变化文件被计入快速路径。
func IsFormatOnlyChange(before, after Fingerprint) bool {
	return before.SHA256 != after.SHA256 &&
		before.FormatKind != "" &&
		before.FormatKind == after.FormatKind &&
		before.FormatSHA256 != "" &&
		before.FormatSHA256 == after.FormatSHA256
}

// foldCRLFInto把chunk中的CRLF折叠为LF并写入dst。
//
// pendingCR表示上一块以CR结束；返回值表示当前块仍以待决CR结束。
// 孤立CR保持原字节，因此不会把真实内容变化误判为换行表示变化。
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
