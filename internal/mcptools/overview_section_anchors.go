package mcptools

import (
	"path"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
)

// overviewSectionAnchor locates one Section marker whose line lies inside a
// Chunk body. It is receipt metadata only: the body stays byte-exact, and the
// ordinal it names comes from the one formal Entry sequence every Chunk
// receipt, Challenge, and Attestation answer consumes.
type overviewSectionAnchor struct {
	// Section is the marker text between the === fences exactly as the body
	// spells it, so the model can find the line by string search.
	Section string `json:"section"`
	// Directory is the repository-relative directory of the Section's first
	// Entry when that Entry is a Code object: "" for the repository root, and
	// also "" when the Section holds no Entry (first_entry_ordinal is then 0)
	// or its first Entry is not a Code object. Always present.
	Directory string `json:"directory"`
	// FirstEntryOrdinal is the formal ordinal of the first Entry after the
	// marker, which may lie in a later Chunk; 0 when no Entry follows before
	// the next marker.
	FirstEntryOrdinal int `json:"first_entry_ordinal"`
	// EntryCount is the number of formal Entries between this marker and the
	// next one, counted over the complete body rather than this Chunk.
	EntryCount int `json:"entry_count"`
}

type overviewSectionMarker struct {
	offset int
	text   string
}

// overviewSectionMarkerOffsets lists every Section marker line of the framed
// body with its byte offset, using the parser's own marker predicate.
func overviewSectionMarkerOffsets(text string) []overviewSectionMarker {
	markers := []overviewSectionMarker{}
	offset := 0
	for offset < len(text) {
		end := strings.IndexByte(text[offset:], '\n')
		line := ""
		next := len(text)
		if end < 0 {
			line = text[offset:]
		} else {
			line = text[offset : offset+end]
			next = offset + end + 1
		}
		if name, ok := index.SectionMarker(line); ok {
			markers = append(markers, overviewSectionMarker{offset: offset, text: name})
		}
		offset = next
	}
	return markers
}

// overviewSectionAnchors returns the anchors for the markers that start inside
// span, in body order. text is the complete framed body and contentStart the
// offset the sequence's ContentOffset values are relative to, exactly as
// planOverviewChunks consumes them, so anchors and Chunk ordinals agree.
func overviewSectionAnchors(text string, contentStart int, sequence []overviewChallengeTarget, span overviewChunkSpan) []overviewSectionAnchor {
	entryOffsets := make([]int, len(sequence))
	for ordinal, object := range sequence {
		entryOffsets[ordinal] = contentStart + object.ContentOffset
	}
	markers := overviewSectionMarkerOffsets(text)
	anchors := []overviewSectionAnchor{}
	for position, marker := range markers {
		if marker.offset < span.Start || marker.offset >= span.End {
			continue
		}
		nextMarker := len(text)
		if position+1 < len(markers) {
			nextMarker = markers[position+1].offset
		}
		first := sort.SearchInts(entryOffsets, marker.offset+1)
		last := sort.SearchInts(entryOffsets, nextMarker)
		anchor := overviewSectionAnchor{Section: marker.text, EntryCount: last - first}
		if last > first {
			anchor.FirstEntryOrdinal = first + 1
			anchor.Directory = overviewSectionDirectory(sequence[first].ObjectIdentity)
		}
		anchors = append(anchors, anchor)
	}
	return anchors
}

// overviewSectionDirectory derives the repository-relative directory of a Code
// object identity ("code:<path>" for Volumes, a bare path for Legacy); other
// identities have no directory.
func overviewSectionDirectory(identity string) string {
	if strings.Contains(identity, "://") {
		return ""
	}
	rel := strings.TrimPrefix(identity, "code:")
	dir := path.Dir(rel)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
