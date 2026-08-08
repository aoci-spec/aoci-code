// MCP认知收据：宿主保留的小型索引身份，不替代宿主对认知可靠性的判断。
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	cognitionReceiptVersion = 1

	cognitionStateValid     = machinecontract.CognitionStateValid
	cognitionStateUncertain = machinecontract.CognitionStateUncertain
	cognitionStateInvalid   = machinecontract.CognitionStateInvalid

	cognitionScopeRepositoryFull = machinecontract.CognitionScopeRepositoryFull
	cognitionStateOwnerHostModel = machinecontract.CognitionStateOwnerHostModel

	cognitionRecallFull                      = machinecontract.CognitionRecallFull
	cognitionRecallNone                      = machinecontract.CognitionRecallNone
	cognitionRecallHostChoiceNoneLocalOrFull = machinecontract.CognitionRecallHostChoiceNoneLocalOrFull
	cognitionRecallHostChoiceLocalOrFull     = machinecontract.CognitionRecallHostChoiceLocalOrFull
)

// cognitionReceipt绑定仓库、索引版本和本次交付范围。
// 普通治理结果只能给出身份和uncertain初态；只有真实交付完整
// Overview或宿主明确声明valid后，才能设置ModelFullReliable。
type cognitionReceipt struct {
	Version               int      `json:"version"`
	RuntimeRepositoryRoot string   `json:"runtime_repository_root"`
	IndexSHA256           string   `json:"index_sha256"`
	MCPServiceVersion     string   `json:"mcp_service_version"`
	Scope                 string   `json:"cognition_scope"`
	State                 string   `json:"cognition_state"`
	StateOwner            string   `json:"state_owner"`
	ModelFullReliable     bool     `json:"model_full_cognition_reliable"`
	RefreshGeneration     int      `json:"refresh_generation,omitempty"`
	LastRefreshEventID    string   `json:"last_refresh_event_id,omitempty"`
	PendingRefreshReasons []string `json:"pending_refresh_reasons,omitempty"`

	LayoutMode         string                   `json:"layout_mode,omitempty"`
	RequestedScope     string                   `json:"requested_scope,omitempty"`
	EffectiveScope     string                   `json:"effective_scope,omitempty"`
	ScopeAvailable     bool                     `json:"scope_available,omitempty"`
	AssetState         string                   `json:"asset_state,omitempty"`
	RootSHA256         string                   `json:"root_sha256,omitempty"`
	MetaSHA256         string                   `json:"meta_sha256,omitempty"`
	DeliveredVolumes   []cognitionVolumeReceipt `json:"delivered_volumes,omitempty"`
	ScopeObjectCount   int                      `json:"scope_object_count,omitempty"`
	ScopeIdentity      string                   `json:"scope_identity,omitempty"`
	CompositeIdentity  string                   `json:"composite_identity,omitempty"`
	ModelScopeReliable bool                     `json:"model_scope_cognition_reliable,omitempty"`
}

type cognitionVolumeReceipt struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	AssetState  string `json:"asset_state"`
	SHA256      string `json:"sha256,omitempty"`
	ObjectCount int    `json:"object_count"`
}

// MarshalJSON keeps Receipt v1 byte-compatible while making every required
// Receipt v2 fact explicit even when its value is false, zero, or an empty
// list. The alias avoids recursive MarshalJSON dispatch.
func (receipt cognitionReceipt) MarshalJSON() ([]byte, error) {
	type receiptAlias cognitionReceipt
	data, err := json.Marshal(receiptAlias(receipt))
	if err != nil || receipt.Version != 2 {
		return data, err
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	values["delivered_volumes"] = append([]cognitionVolumeReceipt{}, receipt.DeliveredVolumes...)
	values["scope_object_count"] = receipt.ScopeObjectCount
	values["scope_available"] = receipt.ScopeAvailable
	values["model_scope_cognition_reliable"] = receipt.ModelScopeReliable
	values["model_full_cognition_reliable"] = receipt.ModelFullReliable
	values["refresh_generation"] = receipt.RefreshGeneration
	values["pending_refresh_reasons"] = append([]string{}, receipt.PendingRefreshReasons...)
	delete(values, "index_sha256")
	return json.Marshal(values)
}

func newCognitionReceipt(root, version, indexText, scope string) cognitionReceipt {
	digest := sha256.Sum256([]byte(indexText))
	if scope == "" {
		scope = cognitionScopeRepositoryFull
	}
	return cognitionReceipt{
		Version:               cognitionReceiptVersion,
		RuntimeRepositoryRoot: root,
		IndexSHA256:           hex.EncodeToString(digest[:]),
		MCPServiceVersion:     version,
		Scope:                 scope,
		State:                 cognitionStateUncertain,
		StateOwner:            cognitionStateOwnerHostModel,
		ModelFullReliable:     false,
	}
}

func newVolumeCognitionReceipt(
	root, version string,
	set *cognition.Set,
	view cognition.ScopeView,
) cognitionReceipt {
	receipt := cognitionReceipt{
		Version: 2, RuntimeRepositoryRoot: root, MCPServiceVersion: version,
		Scope: view.EffectiveScope, State: cognitionStateUncertain,
		StateOwner: cognitionStateOwnerHostModel, LayoutMode: set.LayoutMode,
		RequestedScope: view.RequestedScope, EffectiveScope: view.EffectiveScope,
		ScopeAvailable: view.Available, AssetState: view.AssetState,
		RootSHA256: set.Root.SHA256, MetaSHA256: set.Meta.SHA256,
		ScopeObjectCount: view.ObjectCount, ScopeIdentity: view.ScopeIdentity,
		CompositeIdentity: set.CompositeIdentity,
	}
	return receipt
}

func receiptWithDeliveredVolumes(receipt cognitionReceipt, view cognition.ScopeView) cognitionReceipt {
	receipt.DeliveredVolumes = []cognitionVolumeReceipt{}
	for _, asset := range view.Assets {
		if asset.Descriptor.Kind == "root" {
			continue
		}
		receipt.DeliveredVolumes = append(receipt.DeliveredVolumes, cognitionVolumeReceipt{
			ID: asset.Descriptor.ID, Kind: asset.Descriptor.Kind, Path: asset.Descriptor.Path,
			AssetState: asset.State, SHA256: asset.SHA256, ObjectCount: asset.ObjectCount,
		})
	}
	return receipt
}

func receiptWithRefresh(
	receipt cognitionReceipt,
	generation int,
	lastEventID string,
	pending []string,
) cognitionReceipt {
	receipt.RefreshGeneration = generation
	receipt.LastRefreshEventID = lastEventID
	receipt.PendingRefreshReasons = orderedRefreshReasons(pending)
	return receipt
}

func orderedRefreshReasons(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	ordered := make([]string, 0, len(seen))
	for _, value := range machinecontract.RefreshReasons() {
		if seen[value] {
			ordered = append(ordered, value)
		}
	}
	return ordered
}

func receiptWithState(receipt cognitionReceipt, state string, reliable bool) cognitionReceipt {
	receipt.State = state
	if receipt.Version == 2 {
		receipt.ModelScopeReliable = reliable
		receipt.ModelFullReliable = reliable && receipt.EffectiveScope == cognition.ScopeAll
	} else {
		receipt.ModelFullReliable = reliable
	}
	return receipt
}

// noteSemanticThreshold preserves the machine-derived trigger across the
// existing Maintain -> Apply boundary. Once Apply advances the Baseline the
// live semantic count becomes zero, so the session must retain this small fact
// until one complete Overview consumes it.
func (session *cognitionRefreshSession) noteSemanticThreshold(count, threshold int) {
	if session == nil || threshold <= 0 || count < threshold {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.pendingReasons[machinecontract.RefreshReasonSemanticThreshold] = true
}

// autoRefreshOutcome projects pending receipt facts onto Maintain and Apply
// results without creating another workflow state. The returned status uses
// the existing cognition-refresh vocabulary.
func (session *cognitionRefreshSession) autoRefreshOutcome(
	receipt cognitionReceipt,
	aligned bool,
) (cognitionReceipt, string, []string) {
	if session == nil {
		return receipt, "", nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	reasons := session.pendingReasonList()
	if len(reasons) == 0 {
		return receipt, "", nil
	}
	status := machinecontract.RefreshStatusRequired
	if aligned {
		status = machinecontract.RefreshStatusReadyForOverview
	}
	receipt = receiptWithRefresh(
		receipt,
		session.generation,
		session.lastReceipt.LastRefreshEventID,
		reasons,
	)
	return receipt, status, reasons
}

type cognitionAssessment struct {
	Version       int              `json:"version"`
	State         string           `json:"state"`
	Reason        string           `json:"reason"`
	Recall        string           `json:"recall"`
	Receipt       cognitionReceipt `json:"cognition_receipt"`
	AOCIToolCalls int              `json:"aoci_tool_calls"`
	OverviewReads int              `json:"overview_reads"`
	LocalRecalls  int              `json:"local_recalls"`
}

// assessCognition只确定可机器验证的身份失配，认知可靠性仍由宿主模型声明。
func assessCognition(
	current cognitionReceipt,
	previous *cognitionReceipt,
	modelState string,
	scopeCovered *bool,
) cognitionAssessment {
	result := cognitionAssessment{
		Version:       1,
		State:         cognitionStateInvalid,
		Reason:        "receipt_missing",
		Recall:        cognitionRecallFull,
		Receipt:       current,
		AOCIToolCalls: 1,
	}
	if previous == nil {
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if previous.Version != current.Version {
		result.Reason = "receipt_version_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if previous.RuntimeRepositoryRoot != current.RuntimeRepositoryRoot {
		result.Reason = "repository_root_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if previous.MCPServiceVersion != current.MCPServiceVersion {
		result.Reason = "mcp_service_version_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if current.Version == 1 && previous.IndexSHA256 != current.IndexSHA256 {
		result.Reason = "index_sha256_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if current.Version == 2 && previous.ScopeIdentity != current.ScopeIdentity {
		result.Reason = "cognition_identity_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	if previous.Scope != current.Scope {
		result.Reason = "cognition_scope_changed"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	}
	switch modelState {
	case cognitionStateInvalid:
		result.Reason = "host_model_invalid"
		result.Receipt = receiptWithState(result.Receipt, cognitionStateInvalid, false)
		return result
	case cognitionStateUncertain:
		result.State = cognitionStateUncertain
		result.Reason = "host_model_uncertain"
		result.Recall = cognitionRecallHostChoiceNoneLocalOrFull
		result.Receipt = receiptWithState(result.Receipt, cognitionStateUncertain, false)
		return result
	case cognitionStateValid:
		if scopeCovered != nil && !*scopeCovered {
			result.State = cognitionStateUncertain
			result.Reason = "scope_not_confirmed"
			result.Recall = cognitionRecallHostChoiceLocalOrFull
			result.Receipt = receiptWithState(result.Receipt, cognitionStateUncertain, false)
			return result
		}
		result.State = cognitionStateValid
		result.Reason = "receipt_matches_and_host_model_reliable"
		result.Recall = cognitionRecallNone
		result.Receipt = receiptWithState(result.Receipt, cognitionStateValid, true)
		return result
	default:
		result.State = cognitionStateUncertain
		result.Reason = "host_model_assessment_missing"
		result.Recall = cognitionRecallHostChoiceNoneLocalOrFull
		result.Receipt = receiptWithState(result.Receipt, cognitionStateUncertain, false)
		return result
	}
}
