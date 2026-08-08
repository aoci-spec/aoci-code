// Package indexgen 承载索引生成清单。
//
// Inventory是纯离线、零AI清单:
//
//	磁盘文件集合 - 已索引文件集合 = 待索引候选。
//
// 文件大小、行数、扩展名和empty/binary/oversize画像统一来自
// internal/curation.ProfilePath。indexgen不再维护第二套嗅探算法。
package indexgen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

// Item 是清单中的一个待索引文件。
type Item struct {
	RelPath          string `json:"rel_path"`
	SizeBytes        int64  `json:"size_bytes"`
	Lines            int    `json:"lines"`
	Ext              string `json:"ext"`
	SuggestedSection string `json:"suggested_section"`
	SkipReason       string `json:"skip_reason,omitempty"`
}

// Inventory 是一次清单构建的完整结果。
type Inventory struct {
	Items          []Item `json:"items"`
	DiskTotal      int    `json:"disk_total"`
	IndexedTotal   int    `json:"indexed_total"`
	IndexRoleTotal int    `json:"index_role_total"`
	ObserveTotal   int    `json:"observe_total"`
	ExcludeTotal   int    `json:"exclude_total"`
}

// BuildInventory 构建待索引清单。
func BuildInventory(
	root string,
	cfg *config.Config,
	doc *index.Document,
) (*Inventory, error) {
	diskFiles := []string{}
	observeTotal, excludeTotal := 0, 0
	if cfg.ManagedScope == nil && cfg.CognitionBudget == nil {
		var err error
		diskFiles, err = fs.WalkRepo(root, cfg.WalkOptions())
		if err != nil {
			return nil, fmt.Errorf("遍历仓库失败: %w", err)
		}
	} else {
		state, err := managedstate.Load(root, cfg)
		if err != nil {
			return nil, err
		}
		if state.Evaluation == nil {
			return nil, fmt.Errorf("managed_scope_evaluation_unavailable")
		}
		for _, item := range state.Evaluation.Index {
			diskFiles = append(diskFiles, item.Path)
		}
		observeTotal, excludeTotal = state.Evaluation.ObserveCount, state.Evaluation.ExcludeCount
	}

	index.ResolveRelPaths(
		doc,
		root,
	)
	indexedSet, sections :=
		collectIndexPaths(
			root,
			doc,
		)

	inventory := &Inventory{
		Items:          []Item{},
		DiskTotal:      len(diskFiles),
		IndexedTotal:   0,
		IndexRoleTotal: len(diskFiles),
		ObserveTotal:   observeTotal,
		ExcludeTotal:   excludeTotal,
	}
	for _, rel := range diskFiles {
		if indexedSet[rel] {
			inventory.IndexedTotal++
		}
	}

	for _, rel := range diskFiles {
		if indexedSet[rel] {
			continue
		}

		item, profileErr :=
			profileFile(
				root,
				rel,
				sections,
			)
		if profileErr != nil {
			item = Item{
				RelPath: rel,
				SkipReason: curation.
					ProfileReasonUnreadablePrefix +
					profileErr.Error(),
			}
		}

		inventory.Items = append(
			inventory.Items,
			item,
		)
	}

	sort.Slice(
		inventory.Items,
		func(
			left,
			right int,
		) bool {
			return inventory.Items[left].
				RelPath <
				inventory.Items[right].
					RelPath
		},
	)

	return inventory, nil
}

type sectionRef struct {
	DirRel string
	Name   string
}

func collectIndexPaths(
	root string,
	doc *index.Document,
) (map[string]bool, []sectionRef) {
	indexed := make(
		map[string]bool,
	)
	sections := []sectionRef{}

	for _, section := range doc.Sections {
		if section.AbsPath == "" {
			continue
		}

		for _, entry := range section.Entries {
			if entry.RelPath != "" {
				indexed[entry.RelPath] = true
			}
		}

		dirRel, err := filepath.Rel(
			root,
			section.AbsPath,
		)
		if err != nil ||
			strings.HasPrefix(
				dirRel,
				"..",
			) {
			continue
		}
		if dirRel == "." {
			dirRel = ""
		}

		sections = append(
			sections,
			sectionRef{
				DirRel: filepath.ToSlash(
					dirRel,
				),
				Name: section.Name,
			},
		)
	}

	return indexed, sections
}

// profileFile 把curation物理画像映射为Inventory条目。
func profileFile(
	root,
	rel string,
	sections []sectionRef,
) (Item, error) {
	profile, err := curation.ProfilePath(
		root,
		rel,
	)
	if err != nil {
		return Item{}, err
	}

	return Item{
		RelPath:          profile.Path,
		SizeBytes:        profile.SizeBytes,
		Lines:            profile.Lines,
		Ext:              profile.Ext,
		SuggestedSection: suggestSection(rel, sections),
		SkipReason:       profile.Reason,
	}, nil
}

func suggestSection(
	rel string,
	sections []sectionRef,
) string {
	fileDir := filepath.ToSlash(
		filepath.Dir(rel),
	)
	if fileDir == "." {
		fileDir = ""
	}

	best := ""
	bestLength := -1

	for _, section := range sections {
		if section.DirRel == fileDir {
			return section.Name
		}

		if section.DirRel == "" ||
			strings.HasPrefix(
				fileDir+"/",
				section.DirRel+"/",
			) {
			if len(section.DirRel) >
				bestLength {
				bestLength =
					len(section.DirRel)
				best = section.Name
			}
		}
	}

	if bestLength >= 0 {
		return best
	}
	if fileDir == "" {
		return indexgenMessage("inventory.new_root_section")
	}
	return indexgenMessage("inventory.new_section", fileDir)
}
