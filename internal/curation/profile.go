// Missing文件的确定性物理画像。
//
// 本文件是empty/binary/oversize/unreadable判据的新唯一事实源。
// indexgen和mcptools后续统一消费本包，禁止重新复制嗅探逻辑。
package curation

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

const (
	// BinarySniffBytes 是二进制NUL探测的最大头部读取量。
	BinarySniffBytes = 8000

	// OversizeBytes 是自动读取全文的上限。
	OversizeBytes = 1 << 20
)

// ProfilePath 对仓库相对文件执行确定性画像。
func ProfilePath(
	root,
	rawPath string,
) (Profile, error) {
	cache := baseline.OpenFingerprintCache(root)
	defer cache.Save(false)
	return profilePath(cache, root, rawPath)
}

// profilePath is ProfilePath over a cache the caller keeps open across a
// batch. The digest, the binary verdict, and the line count all come from the
// bytes the fingerprint already read: a NUL within the first BinarySniffBytes
// is exactly the file hashFile leaves without a normalized digest, so a file
// under OversizeBytes is binary if and only if that digest is absent, and no
// second read of the file is needed for the verdict.
func profilePath(
	cache *baseline.FingerprintCache,
	root,
	rawPath string,
) (Profile, error) {
	rel, err := afs.NormalizeRelPath(rawPath)
	if err != nil {
		return Profile{}, err
	}

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)

	info, err := os.Stat(absolutePath)
	if err != nil {
		return Profile{}, err
	}
	if info.IsDir() {
		return Profile{}, fmt.Errorf(
			"目标是目录而非文件: %s",
			rel,
		)
	}

	fingerprint, err := cache.HashFile(
		absolutePath,
		rel,
		baseline.Fingerprint{},
	)
	if err != nil {
		return Profile{}, err
	}

	profile := Profile{
		Path:         rel,
		SourceSHA256: fingerprint.SHA256,
		SizeBytes:    info.Size(),
		Ext:          filepath.Ext(rel),
	}

	switch {
	case info.Size() == 0:
		profile.Reason = ProfileReasonEmpty
		return profile, nil

	case info.Size() > OversizeBytes:
		profile.Reason = ProfileReasonOversize
		return profile, nil
	}

	if fingerprint.NormalizedSHA256 == "" {
		profile.Reason = ProfileReasonBinary
		return profile, nil
	}

	lines, err := cache.FileLines(
		absolutePath,
		rel,
	)
	if err != nil {
		return Profile{}, err
	}
	profile.Lines = lines

	return profile, nil
}

// BuildProfiles 对指定路径集合建立可见、稳定的文件画像。
//
// 单文件失败不会让整批事实消失，而是形成unreadable:*画像。
func BuildProfiles(
	root string,
	paths []string,
) map[string]Profile {
	result := make(
		map[string]Profile,
		len(paths),
	)
	cache := baseline.OpenFingerprintCache(root)
	defer cache.Save(false)

	for _, rawPath := range paths {
		profile, err := profilePath(
			cache,
			root,
			rawPath,
		)
		if err != nil {
			result[rawPath] = Profile{
				Path: rawPath,
				Reason: ProfileReasonUnreadablePrefix +
					err.Error(),
			}
			continue
		}

		result[profile.Path] = profile
	}

	return result
}
