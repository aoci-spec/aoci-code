package volumegovernance

import (
	"path/filepath"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// EScaleItem is one Code Entry whose E scale letter disagrees with the line
// count of its source under the Meta's own thresholds.
type EScaleItem struct {
	ObjectRef string   `json:"object_ref"`
	Path      string   `json:"path"`
	Actual    string   `json:"actual"`
	Expected  []string `json:"expected"`
	FileLines int      `json:"file_lines"`
}

// EScaleFacts is the informational E scale audit over the Code Volume. It
// never enters findings or the exit code: a file growing across a band is
// ordinary, and the letter is corrected on the Entry's next drift or through
// cognition optimization, which ranks these Entries for review.
type EScaleFacts struct {
	Checked    int          `json:"checked"`
	Mismatched int          `json:"mismatched"`
	Items      []EScaleItem `json:"items"`
}

// AssessEScale reads every indexed source to count its lines, so Verify and
// Check call it and Assess, which runs on every Maintain and Overview, does
// not. Thresholds come from the Meta's code dictionary; without them, or for
// an unreadable source, nothing is judged.
func AssessEScale(root string, set *cognition.Set) EScaleFacts {
	facts := EScaleFacts{Items: []EScaleItem{}}
	if set == nil || len(set.Meta.Raw) == 0 {
		return facts
	}
	asset := enabledAsset(set, cognition.ScopeCode)
	if asset == nil {
		return facts
	}
	thresholds := index.ExtractEScaleThresholds(index.ScopedDictionaryText(string(set.Meta.Raw), "code"))
	if !thresholds.HasThresholds() {
		return facts
	}
	for _, object := range asset.Objects {
		if object.Entry == nil || object.Entry.RelPath == "" || !index.ShouldCheckEScalePath(object.Entry.RelPath) {
			continue
		}
		lines, err := afs.CountFileLines(filepath.Join(root, filepath.FromSlash(object.Entry.RelPath)))
		if err != nil {
			continue
		}
		facts.Checked++
		if mismatch := index.CheckEScaleDetail(object.CanonicalLine, lines, thresholds); mismatch != nil {
			facts.Mismatched++
			facts.Items = append(facts.Items, EScaleItem{ObjectRef: object.CanonicalRef, Path: object.Entry.RelPath,
				Actual: mismatch.Actual, Expected: append([]string{}, mismatch.Expected...), FileLines: mismatch.FileLines})
		}
	}
	sort.Slice(facts.Items, func(i, j int) bool { return facts.Items[i].Path < facts.Items[j].Path })
	return facts
}
