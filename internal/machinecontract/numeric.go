// Package machinecontract owns stable numeric limits shared by deterministic
// validation and model-visible contracts.
package machinecontract

import "fmt"

const (
	SQuotaHighMinC  = 8
	SQuotaHighMaxC  = 9
	SQuotaHighRunes = 600
	SQuotaMidMinC   = 4
	SQuotaMidMaxC   = 7
	SQuotaMidRunes  = 500
	SQuotaLowMinC   = 1
	SQuotaLowMaxC   = 3
	SQuotaLowRunes  = 50

	// Object FRAS v2 density limits apply only to Volumes v1 objects and new
	// candidates. Legacy v1 Entries remain fully compatible. These values are
	// the single machine authority consumed by deterministic validation and
	// rendered documentation; validators never truncate or rewrite semantics.
	ObjectFRASV2FMaxRunes = 160
	ObjectFRASV2RMaxRunes = 360
	ObjectFRASV2RMaxItems = 8
	ObjectFRASV2AMaxRunes = 400
	ObjectFRASV2AMaxItems = 6

	HeaderRequestMaxBytes   = 4 << 20
	HeaderTextMaxBytes      = 512 << 10
	EntriesRequestMaxBytes  = 16 << 20
	EntriesBatchMaxItems    = 200
	CurationRequestMaxBytes = 8 << 20
	CurationBatchMaxItems   = 200

	// CognitionRefreshThresholdDefault is the team-policy default used when a
	// repository predates the long-running cognition refresh contract. The
	// bounds reject zero (which would refresh forever) and impractically large
	// values while still covering very large repositories.
	CognitionRefreshThresholdDefault = 30
	CognitionRefreshThresholdMin     = 1
	CognitionRefreshThresholdMax     = 100000

	// Overview delivery is transport framing only. These values do not enter
	// Index, Baseline, Managed Scope, cognition-budget, or semantic identity.
	OverviewChunkTokensDefault = 600000
	OverviewChunkTokensMin     = 4000
	OverviewChunkTokensMax     = 600000

	// Code Cognition authoring batches: how many candidates one Maintain asks
	// the model to author in a single aoci_update_entry call. The ceiling is
	// the wire maximum a call may carry (EntriesBatchMaxItems); the default is
	// sized for what a model authors inline in one call and for a Maintain
	// response that fits ordinary host tool-result windows. A team raises it
	// through configuration when its host and model can carry more.
	CodeCognitionBatchEntriesDefault = 20
	CodeCognitionBatchEntriesMin     = 1
	CodeCognitionBatchEntriesMax     = EntriesBatchMaxItems

	// MaintainTransportListLimit bounds the per-item enumerations Maintain
	// carries for situational awareness (governance findings, drift lists,
	// review closure). Beyond it the response keeps the leading sample plus the
	// complete count; the actionable list is always the candidate set, and
	// Verify and Check keep reporting every item.
	MaintainTransportListLimit = 20

	// Database Cognition batches are bounded by object count and the exact
	// canonical Evidence bytes delivered to the model. The planner never
	// truncates Evidence or predicts model tokens; a single oversized table is
	// returned alone so the host can make an explicit decision with full facts.
	// The byte default is the operative gate under real table sizes: at a
	// few KB per canonical table it keeps a batch inside a host window while
	// narrow tables still reach the object count.
	DatabaseCognitionBatchObjectsDefault       = 20
	DatabaseCognitionBatchObjectsMin           = 1
	DatabaseCognitionBatchObjectsMax           = 200
	DatabaseCognitionBatchEvidenceBytesDefault = 64 << 10
	DatabaseCognitionBatchEvidenceBytesMin     = 32 << 10
	DatabaseCognitionBatchEvidenceBytesMax     = 16 << 20
)

// FRASV2Limits is the immutable density contract for new cognition objects.
type FRASV2Limits struct {
	FMaxRunes int
	RMaxRunes int
	RMaxItems int
	AMaxRunes int
	AMaxItems int
}

func ObjectFRASV2Limits() FRASV2Limits {
	return FRASV2Limits{
		FMaxRunes: ObjectFRASV2FMaxRunes,
		RMaxRunes: ObjectFRASV2RMaxRunes,
		RMaxItems: ObjectFRASV2RMaxItems,
		AMaxRunes: ObjectFRASV2AMaxRunes,
		AMaxItems: ObjectFRASV2AMaxItems,
	}
}

// SQuotaBand is one default importance range and its S-field rune limit.
type SQuotaBand struct {
	MinC     int
	MaxC     int
	MaxRunes int
}

// DefaultSQuotaBands returns the canonical bands in descending importance
// order. The returned slice is new, so callers cannot mutate package state.
func DefaultSQuotaBands() []SQuotaBand {
	return []SQuotaBand{
		{MinC: SQuotaHighMinC, MaxC: SQuotaHighMaxC, MaxRunes: SQuotaHighRunes},
		{MinC: SQuotaMidMinC, MaxC: SQuotaMidMaxC, MaxRunes: SQuotaMidRunes},
		{MinC: SQuotaLowMinC, MaxC: SQuotaLowMaxC, MaxRunes: SQuotaLowRunes},
	}
}

// DefaultSQuotaForC returns the fallback S-field limit for an importance value.
func DefaultSQuotaForC(importance int) int {
	switch {
	case importance >= SQuotaHighMinC:
		return SQuotaHighRunes
	case importance >= SQuotaMidMinC:
		return SQuotaMidRunes
	default:
		return SQuotaLowRunes
	}
}

// NumericTextValues contains derived values accepted by text/template assets.
// Human-readable sizes and quota clauses are formatted from the machine values.
type NumericTextValues struct {
	CurationMaxBodyBytes   int
	CurationMaxBodyHuman   string
	CurationMaxDecisions   int
	EntriesMaxBodyBytes    int
	EntriesMaxBodyHuman    string
	EntriesMaxEntries      int
	HeaderMaxBodyBytes     int
	HeaderMaxBodyHuman     string
	HeaderMaxHeaderBytes   int
	HeaderMaxHeaderHuman   string
	SQuotaDefaultCompact   string
	SQuotaDefaultExample   string
	SQuotaDefaultSpaced    string
	SQuotaDefaultWithUnits string
}

// NumericText returns all model-visible numeric substitutions from the
// canonical machine values.
func NumericText() NumericTextValues {
	return NumericTextValues{
		CurationMaxBodyBytes:   CurationRequestMaxBytes,
		CurationMaxBodyHuman:   IECBytes(CurationRequestMaxBytes),
		CurationMaxDecisions:   CurationBatchMaxItems,
		EntriesMaxBodyBytes:    EntriesRequestMaxBytes,
		EntriesMaxBodyHuman:    IECBytes(EntriesRequestMaxBytes),
		EntriesMaxEntries:      EntriesBatchMaxItems,
		HeaderMaxBodyBytes:     HeaderRequestMaxBytes,
		HeaderMaxBodyHuman:     IECBytes(HeaderRequestMaxBytes),
		HeaderMaxHeaderBytes:   HeaderTextMaxBytes,
		HeaderMaxHeaderHuman:   IECBytes(HeaderTextMaxBytes),
		SQuotaDefaultCompact:   formatSQuota("", " "),
		SQuotaDefaultExample:   formatSQuotaBand(DefaultSQuotaBands()[0], ""),
		SQuotaDefaultSpaced:    formatSQuota(" 字", " / "),
		SQuotaDefaultWithUnits: formatSQuota("字", " / "),
	}
}

// IECBytes formats exact KiB and MiB values used by public contracts.
func IECBytes(value int) string {
	const (
		kib = 1 << 10
		mib = 1 << 20
	)

	switch {
	case value > 0 && value%mib == 0:
		return fmt.Sprintf("%d MiB", value/mib)
	case value > 0 && value%kib == 0:
		return fmt.Sprintf("%d KiB", value/kib)
	default:
		return fmt.Sprintf("%d bytes", value)
	}
}

func formatSQuota(unit, separator string) string {
	bands := DefaultSQuotaBands()
	result := ""
	for position, band := range bands {
		if position > 0 {
			result += separator
		}
		result += formatSQuotaBand(band, unit)
	}
	return result
}

// FormatSQuota derives the canonical bands with locale-owned presentation
// units and separators. Numeric boundaries remain owned by this package.
func FormatSQuota(unit, separator string) string {
	return formatSQuota(unit, separator)
}

func formatSQuotaBand(band SQuotaBand, unit string) string {
	return fmt.Sprintf(
		"C%d-%d≤%d%s",
		band.MaxC,
		band.MinC,
		band.MaxRunes,
		unit,
	)
}
