package index

// NeutralCodeRootHeader anchors new Code volumes without recording the host's
// repository path. The description distinguishes it from historical coordinates.
const NeutralCodeRootHeader = "===project/.code/==="

const neutralCodeRoot = "/.code"

func isNeutralCodeRoot(section *Section) bool {
	return section != nil && section.HeaderLine == NeutralCodeRootHeader
}

// neutralSectionReadings recognizes only the explicitly marked family. It must
// precede runtime matching: a checkout at /.code/src still has a src directory.
// A marked family with an outside section fails closed, never falls back to a
// mixture of neutral coordinates and runtime paths.
func neutralSectionReadings(doc *Document) (map[*Section]sectionReading, bool) {
	if !isNeutralCodeRoot(firstDirectorySection(doc)) {
		return nil, false
	}
	readings := make(map[*Section]sectionReading)
	for _, section := range doc.Sections {
		if section.AbsPath == "" {
			continue
		}
		rel, ok := relUnder(normalizeRootPath(section.AbsPath), neutralCodeRoot)
		if !ok {
			return nil, true
		}
		readings[section] = sectionReading{rel: rel}
	}
	return readings, true
}
