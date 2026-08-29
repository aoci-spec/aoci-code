package cognitionbudget

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type EntryCost struct {
	Path        string `json:"path"`
	Importance  int    `json:"importance"`
	FTokens     int    `json:"f_tokens"`
	RTokens     int    `json:"r_tokens"`
	ATokens     int    `json:"a_tokens"`
	STokens     int    `json:"s_tokens"`
	TotalTokens int    `json:"entry_total_tokens"`
}

type Violation struct {
	Code       string `json:"code"`
	Path       string `json:"path,omitempty"`
	Field      string `json:"field,omitempty"`
	Importance int    `json:"importance,omitempty"`
	Actual     int    `json:"actual_tokens"`
	Maximum    int    `json:"max_tokens"`
}

type Report struct {
	Version          string           `json:"version"`
	Mode             string           `json:"mode"`
	Status           string           `json:"budget_status"`
	WholeIndexTokens int              `json:"whole_index_tokens"`
	TargetTokens     int              `json:"target_tokens"`
	WarningTokens    int              `json:"warning_tokens"`
	MaxTokens        int              `json:"max_tokens"`
	HeaderTokens     int              `json:"header_tokens"`
	FTokens          int              `json:"f_tokens"`
	RTokens          int              `json:"r_tokens"`
	ATokens          int              `json:"a_tokens"`
	STokens          int              `json:"s_tokens"`
	StructureTokens  int              `json:"structure_tokens"`
	EntryCount       int              `json:"entry_count"`
	LargestEntries   []EntryCost      `json:"largest_entries"`
	LargestR         []EntryCost      `json:"largest_r"`
	LargestS         []EntryCost      `json:"largest_s"`
	Violations       []Violation      `json:"violations"`
	Policy           WholeIndexPolicy `json:"whole_index_policy"`
}

type Validation struct {
	Version    string      `json:"version"`
	Allowed    bool        `json:"allowed"`
	Mode       string      `json:"mode"`
	Report     Report      `json:"report"`
	Violations []Violation `json:"violations"`
}

type Projection struct {
	Version                   string      `json:"version"`
	Allowed                   bool        `json:"allowed"`
	Mode                      string      `json:"mode"`
	CurrentTokens             int         `json:"current_tokens"`
	ProjectedWholeIndexTokens int         `json:"projected_whole_index_tokens"`
	TargetTokens              int         `json:"target_tokens"`
	WarningTokens             int         `json:"warning_tokens"`
	MaxTokens                 int         `json:"max_tokens"`
	BatchDeltaTokens          int         `json:"batch_delta_tokens"`
	LargestR                  []EntryCost `json:"largest_r"`
	LargestS                  []EntryCost `json:"largest_s"`
	LargestEntries            []EntryCost `json:"largest_entries"`
	SuggestedCompression      []EntryCost `json:"suggested_compression"`
	Violations                []Violation `json:"violations"`
}

func EstimateTokens(data []byte) int { return EstimateTokensOfSize(len(data)) }

// EstimateTokensOfSize is the same estimate for a byte count the caller already
// has, so a caller summing several assets never has to restate the formula.
func EstimateTokensOfSize(byteCount int) int { return byteCount / 3 }

func Build(repositoryRoot string, raw []byte, policy Policy) (*Report, error) {
	normalized, err := Normalize(policy)
	if err != nil {
		return nil, err
	}
	doc, warnings := index.Parse(string(raw))
	if len(warnings) != 0 {
		return nil, fmt.Errorf("cognition_budget_index_parse_warnings: %d", len(warnings))
	}
	index.ResolveRelPaths(doc, repositoryRoot)
	report := &Report{Version: machinecontract.CognitionBudgetReportV1, Mode: normalized.Mode,
		WholeIndexTokens: EstimateTokens(raw), TargetTokens: normalized.WholeIndex.TargetTokens,
		WarningTokens: normalized.WholeIndex.WarningTokens, MaxTokens: normalized.WholeIndex.MaxTokens,
		Policy: normalized.WholeIndex, LargestEntries: []EntryCost{}, LargestR: []EntryCost{}, LargestS: []EntryCost{}, Violations: []Violation{}}
	headerBytes := 0
	if len(doc.Sections) > 0 {
		lines := strings.Split(string(raw), "\n")
		for lineIndex := 0; lineIndex < doc.Sections[0].StartLine-1 && lineIndex < len(lines); lineIndex++ {
			headerBytes += len([]byte(lines[lineIndex])) + 1
		}
	} else {
		headerBytes = len(raw)
	}
	report.HeaderTokens = headerBytes / 3
	entries := []EntryCost{}
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			importance := entryImportance(entry)
			cost := EntryCost{Path: entry.RelPath, Importance: importance, FTokens: len([]byte(entry.F)) / 3,
				RTokens: len([]byte(entry.R)) / 3, ATokens: len([]byte(entry.Api)) / 3, STokens: len([]byte(entry.S)) / 3,
				TotalTokens: len([]byte(entry.FullLine)) / 3}
			entries = append(entries, cost)
			report.FTokens += cost.FTokens
			report.RTokens += cost.RTokens
			report.ATokens += cost.ATokens
			report.STokens += cost.STokens
			if band, ok := LimitFor(normalized.R, importance); ok && cost.RTokens > band.MaxTokens {
				report.Violations = append(report.Violations, Violation{Code: "entry_field_budget_exceeded", Path: cost.Path, Field: "R", Importance: importance, Actual: cost.RTokens, Maximum: band.MaxTokens})
			}
			if band, ok := LimitFor(normalized.S, importance); ok && cost.STokens > band.MaxTokens {
				report.Violations = append(report.Violations, Violation{Code: "entry_field_budget_exceeded", Path: cost.Path, Field: "S", Importance: importance, Actual: cost.STokens, Maximum: band.MaxTokens})
			}
		}
	}
	report.EntryCount = len(entries)
	report.StructureTokens = report.WholeIndexTokens - report.HeaderTokens - report.FTokens - report.RTokens - report.ATokens - report.STokens
	if report.StructureTokens < 0 {
		report.StructureTokens = 0
	}
	report.LargestEntries = topEntries(entries, func(value EntryCost) int { return value.TotalTokens })
	report.LargestR = topEntries(entries, func(value EntryCost) int { return value.RTokens })
	report.LargestS = topEntries(entries, func(value EntryCost) int { return value.STokens })
	report.Status = statusFor(report.WholeIndexTokens, normalized.WholeIndex)
	if report.WholeIndexTokens > normalized.WholeIndex.MaxTokens {
		report.Violations = append(report.Violations, Violation{Code: "whole_index_budget_exceeded", Actual: report.WholeIndexTokens, Maximum: normalized.WholeIndex.MaxTokens})
	}
	sort.Slice(report.Violations, func(i, j int) bool {
		left, right := report.Violations[i], report.Violations[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Field < right.Field
	})
	return report, nil
}

// RebindWholeIndexTokens replaces the Whole-Index total with a figure measured
// over a complete multi-asset Volumes v1 set, and recomputes everything derived
// from it.
//
// Build measures one asset, which is correct for a Legacy single-file index and
// wrong for Volumes v1: there cfg.IndexPath is the Root pointer, a few hundred
// bytes that name the other assets. A report built over it announces a
// Whole-Index of about a hundred tokens and no Entries at all, which is what
// `aoci scope status` reported on a real 58166-token repository — the one command
// every blocked budget path hands the operator.
//
// Callers Build over the object Volume so the per-field costs and per-Entry
// violations are real, then rebind the total. The difference between the total
// and the measured fields lands in StructureTokens, which is exactly what the
// Root and Meta assets are.
func (r *Report) RebindWholeIndexTokens(tokens int, policy Policy) error {
	normalized, err := Normalize(policy)
	if err != nil {
		return err
	}
	r.WholeIndexTokens = tokens
	r.StructureTokens = tokens - r.HeaderTokens - r.FTokens - r.RTokens - r.ATokens - r.STokens
	if r.StructureTokens < 0 {
		r.StructureTokens = 0
	}
	r.Status = statusFor(tokens, normalized.WholeIndex)
	kept := make([]Violation, 0, len(r.Violations)+1)
	for _, violation := range r.Violations {
		if violation.Code != "whole_index_budget_exceeded" {
			kept = append(kept, violation)
		}
	}
	if tokens > normalized.WholeIndex.MaxTokens {
		kept = append(kept, Violation{Code: "whole_index_budget_exceeded",
			Actual: tokens, Maximum: normalized.WholeIndex.MaxTokens})
	}
	sort.Slice(kept, func(i, j int) bool {
		left, right := kept[i], kept[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Field < right.Field
	})
	r.Violations = kept
	return nil
}

func Validate(repositoryRoot string, raw []byte, policy Policy) (*Validation, error) {
	report, err := Build(repositoryRoot, raw, policy)
	if err != nil {
		return nil, err
	}
	allowed := report.Mode != machinecontract.BudgetModeEnforce || len(report.Violations) == 0
	return &Validation{Version: machinecontract.CognitionBudgetValidationV1, Allowed: allowed, Mode: report.Mode,
		Report: *report, Violations: append([]Violation{}, report.Violations...)}, nil
}

func ValidateProjected(repositoryRoot string, current, projected []byte, policy Policy) (*Projection, error) {
	currentReport, err := Build(repositoryRoot, current, policy)
	if err != nil {
		return nil, err
	}
	projectedReport, err := Build(repositoryRoot, projected, policy)
	if err != nil {
		return nil, err
	}
	suggested := []EntryCost{}
	seen := map[string]bool{}
	allLargest := append([]EntryCost{}, projectedReport.LargestEntries...)
	allLargest = append(allLargest, projectedReport.LargestR...)
	allLargest = append(allLargest, projectedReport.LargestS...)
	for _, violation := range projectedReport.Violations {
		if violation.Path == "" || seen[violation.Path] {
			continue
		}
		for _, entry := range allLargest {
			if entry.Path == violation.Path {
				suggested = append(suggested, entry)
				seen[entry.Path] = true
				break
			}
		}
	}
	for _, entry := range projectedReport.LargestEntries {
		if len(suggested) >= 10 {
			break
		}
		if !seen[entry.Path] {
			suggested = append(suggested, entry)
			seen[entry.Path] = true
		}
	}
	return &Projection{Version: machinecontract.CognitionBudgetValidationV1, Mode: projectedReport.Mode,
		Allowed:       projectedReport.Mode != machinecontract.BudgetModeEnforce || len(projectedReport.Violations) == 0,
		CurrentTokens: currentReport.WholeIndexTokens, ProjectedWholeIndexTokens: projectedReport.WholeIndexTokens,
		TargetTokens: projectedReport.TargetTokens, WarningTokens: projectedReport.WarningTokens, MaxTokens: projectedReport.MaxTokens,
		BatchDeltaTokens: projectedReport.WholeIndexTokens - currentReport.WholeIndexTokens,
		LargestR:         projectedReport.LargestR, LargestS: projectedReport.LargestS, LargestEntries: projectedReport.LargestEntries,
		SuggestedCompression: suggested, Violations: append([]Violation{}, projectedReport.Violations...)}, nil
}

func ValidateEntry(line string, policy Policy) []Violation {
	normalized, err := Normalize(policy)
	if err != nil {
		return []Violation{{Code: "cognition_budget_policy_invalid"}}
	}
	entry, ok := index.ParseEntryLine(line, 1)
	if !ok {
		return []Violation{}
	}
	importance := entryImportance(entry)
	result := []Violation{}
	for _, item := range []struct {
		name, value string
		bands       []FieldBand
	}{{"R", entry.R, normalized.R}, {"S", entry.S, normalized.S}} {
		actual := len([]byte(item.value)) / 3
		if band, found := LimitFor(item.bands, importance); found && actual > band.MaxTokens {
			result = append(result, Violation{Code: "entry_field_budget_exceeded", Field: item.name, Importance: importance, Actual: actual, Maximum: band.MaxTokens})
		}
	}
	return result
}

func entryImportance(entry *index.Entry) int {
	if entry == nil {
		return 0
	}
	value := entry.TagsParsed["C"]
	importance, err := strconv.Atoi(value)
	if err != nil || importance < 1 || importance > 9 {
		return 0
	}
	return importance
}

func topEntries(entries []EntryCost, score func(EntryCost) int) []EntryCost {
	values := append([]EntryCost{}, entries...)
	sort.Slice(values, func(i, j int) bool {
		left, right := score(values[i]), score(values[j])
		if left != right {
			return left > right
		}
		return values[i].Path < values[j].Path
	})
	if len(values) > 10 {
		values = values[:10]
	}
	return values
}

func statusFor(tokens int, policy WholeIndexPolicy) string {
	switch {
	case tokens > policy.MaxTokens:
		return machinecontract.BudgetStatusExceeded
	case tokens >= policy.WarningTokens:
		return machinecontract.BudgetStatusWarning
	case tokens > policy.TargetTokens:
		return machinecontract.BudgetStatusNearBudget
	default:
		return machinecontract.BudgetStatusHealthy
	}
}

func Summary(report *Report) string {
	if report == nil {
		return ""
	}
	return fmt.Sprintf("whole_index=%d target=%d warning=%d max=%d status=%s", report.WholeIndexTokens,
		report.TargetTokens, report.WarningTokens, report.MaxTokens, report.Status)
}
