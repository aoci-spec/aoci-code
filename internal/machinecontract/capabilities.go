package machinecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

const (
	ProductName                   = "AOCI-CODE"
	BinaryName                    = "aoci"
	CanonicalRepository           = "https://github.com/aoci-spec/aoci-code"
	GoModulePath                  = "github.com/aoci-spec/aoci-code"
	MCPServerName                 = "aoci-code"
	CapabilityManifestV1          = "aoci-capability-manifest/v1"
	SafeInventoryV2               = "safe-inventory/v2"
	BusinessSourceManifestV1      = "business-source-manifest/v1"
	OverviewDeliveryReceiptV1     = "overview-delivery-receipt/v1"
	OverviewChunkReceiptV1        = "overview-chunk-receipt/v1"
	ModelCognitionAttestationV1   = "model-cognition-attestation/v1"
	CognitionStateV2              = "cognition-state/v2"
	CognitionOnboardingSessionV1  = "cognition-onboarding-session/v1"
	CognitionOnboardingSessionV2  = "cognition-onboarding-session/v2"
	CognitionOnboardingBatchV1    = "cognition-onboarding-authoring-batch/v1"
	CognitionOnboardingBatchV2    = "cognition-onboarding-authoring-batch/v2"
	CognitionOnboardingCompleteV1 = "cognition-onboarding-completion/v1"
	CognitionOnboardingCompleteV2 = "cognition-onboarding-completion/v2"
	HostInteractionV1             = "aoci-host-interaction/v1"
	MCPProtocolCurrent            = "2025-11-25"
)

var mcpToolNames = []string{
	"aoci_get_entries", "aoci_header", "aoci_maintain", "aoci_overview", "aoci_remove_entry",
	"aoci_report", "aoci_rules", "aoci_search", "aoci_update_entry",
}

func MCPToolNames() []string {
	result := append([]string{}, mcpToolNames...)
	sort.Strings(result)
	return result
}

func MCPToolNameIdentity() string {
	digest := sha256.Sum256([]byte(strings.Join(MCPToolNames(), "\n") + "\n"))
	return hex.EncodeToString(digest[:])
}
