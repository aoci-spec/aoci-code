package mcptools

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// cognitionChangeEnvelope is derived by AOCI after the model supplies the
// complete candidate set. It is deliberately not part of any CLI or MCP input.
type cognitionChangeEnvelope struct {
	ChangeObject  string
	ChangeObjects []string
	Volume        string
	Volumes       []string
	WriteSet      []string
	GuardSet      []string
	Strategy      string
	Reasons       []cognition.ImpactReason
	guards        map[string]cognitionGuard
}

type cognitionGuard struct {
	Path   string
	SHA256 string
}

func resolveSingleVolumeChangeEnvelope(
	set *cognition.Set,
	candidate cognition.ImpactCandidate,
) (*cognitionChangeEnvelope, *Fail) {
	if candidate.OriginalCandidateIndex == 0 {
		candidate.OriginalCandidateIndex = 1
	}
	return resolveCognitionChangeEnvelope(set, []cognition.ImpactCandidate{candidate})
}

func resolveCognitionChangeEnvelope(
	set *cognition.Set,
	candidates []cognition.ImpactCandidate,
) (*cognitionChangeEnvelope, *Fail) {
	if len(candidates) == 0 || len(candidates) > machinecontract.EntriesBatchMaxItems {
		return nil, &Fail{Code: errCrossVolumeWriteNotSupported, Msg: writeMessage("entry.volume.cross_write_not_supported")}
	}
	for _, candidate := range candidates {
		if candidate.Change != cognition.ImpactChangeUpdate &&
			!(candidate.Change == cognition.ImpactChangeCreate &&
				(cognition.IsCanonicalDatabaseRef(candidate.ObjectRef) || strings.HasPrefix(candidate.ObjectRef, "code:"))) &&
			!(candidate.Change == cognition.ImpactChangeDelete && candidate.VolumeID == cognition.ScopeCode &&
				strings.HasPrefix(candidate.ObjectRef, "code:")) {
			return nil, &Fail{Code: errVolumeReadOnly, Msg: writeMessage("entry.volume.target_not_supported", candidate.VolumeID)}
		}
	}
	impact, err := cognition.ResolveImpact(set, candidates)
	if err != nil || len(impact.Findings) > 0 {
		detail := "impact_resolution_failed"
		if len(impact.Findings) > 0 {
			detail = impact.Findings[0].Code
		}
		findings := LocalizeRepairFindings(impact.Findings)
		return nil, &Fail{
			Code:       errImpactResolutionFailed,
			Msg:        writeMessage("entry.volume.impact_failed", detail),
			Hint:       writeMessage("entry.volume.hint.regenerate_candidate"),
			Findings:   findings,
			Repairable: repairableImpactFindings(findings),
		}
	}
	if len(impact.WriteSet) == 0 || len(impact.WriteSet) > 2 {
		return nil, &Fail{
			Code: errCrossVolumeWriteNotSupported,
			Msg:  writeMessage("entry.volume.cross_write_not_supported"),
		}
	}
	for _, volumeID := range impact.WriteSet {
		if volumeID != "code" && volumeID != "database" {
			return nil, &Fail{Code: errVolumeReadOnly, Msg: writeMessage("entry.volume.target_not_supported", volumeID), Hint: writeMessage("mcp.error.volume_read_only_hint")}
		}
		asset := set.Volumes[volumeID]
		if asset == nil || asset.State != cognition.AssetPresent {
			return nil, &Fail{Code: errVolumeReadOnly, Msg: writeMessage("entry.volume.target_not_supported", volumeID), Hint: writeMessage("mcp.error.volume_read_only_hint")}
		}
	}
	for _, guardID := range impact.GuardSet {
		if guardID != "root" && guardID != "meta" && guardID != "code" && guardID != "database" {
			return nil, &Fail{Code: errCrossVolumeGuardRequired, Msg: writeMessage("entry.volume.cross_guard_required", guardID), Hint: writeMessage("entry.volume.hint.cross_guard_required")}
		}
	}
	if impact.Upgrade {
		return nil, &Fail{
			Code: errCrossVolumeWriteNotSupported,
			Msg:  writeMessage("entry.volume.cross_write_not_supported"),
		}
	}

	guards := map[string]cognitionGuard{}
	for _, required := range impact.GuardSet {
		var asset *cognition.Asset
		switch required {
		case "root":
			asset = &set.Root
		case "meta":
			asset = &set.Meta
		default:
			asset = set.Volumes[required]
		}
		if asset != nil {
			guards[required] = cognitionGuard{Path: asset.Descriptor.Path, SHA256: asset.SHA256}
		}
		guard, ok := guards[required]
		if !ok || guard.Path == "" || guard.SHA256 == "" {
			return nil, &Fail{
				Code: errImpactResolutionFailed,
				Msg:  writeMessage("entry.volume.guard_unavailable", required),
			}
		}
	}
	changeObjects := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		changeObjects = append(changeObjects, candidate.ObjectRef)
	}
	sort.Strings(changeObjects)
	volumes := append([]string{}, impact.WriteSet...)
	return &cognitionChangeEnvelope{
		ChangeObject:  strings.Join(changeObjects, ","),
		ChangeObjects: changeObjects,
		Volume:        strings.Join(volumes, ","),
		Volumes:       volumes,
		WriteSet:      append([]string{}, impact.WriteSet...),
		GuardSet:      append([]string{}, impact.GuardSet...),
		Strategy:      cognition.ImpactStrategyDependencyClosure,
		Reasons:       append([]cognition.ImpactReason{}, impact.Reasons...),
		guards:        guards,
	}, nil
}

func volumeCodeRepoContext(root string, loaded *cognitionRepoCtx) *repoCtx {
	code := loaded.set.Volumes["code"]
	cfg := *loaded.cfg
	cfg.IndexPath = code.Descriptor.Path
	return &repoCtx{
		cfg:   &cfg,
		paths: config.AOCIPaths(root, code.Descriptor.Path),
		text:  string(code.Raw),
		doc:   code.Document,
		bl:    loaded.bl,
	}
}

// canonicalVolumeCandidateLine applies only the existing path-to-section
// identity normalization. It never creates or rewrites tag or FRAS semantics.
func canonicalVolumeCandidateLine(rel, raw string) string {
	line := index.StripFences(raw)
	filenameEnd := strings.Index(line, "[")
	if filenameEnd <= 0 {
		return line
	}
	if strings.TrimSpace(line[:filenameEnd]) == rel {
		return path.Base(rel) + line[filenameEnd:]
	}
	return line
}

func externalGuardMismatch(root string, envelope *cognitionChangeEnvelope) (string, error) {
	if envelope == nil {
		return "", nil
	}
	writeSet := map[string]bool{}
	for _, volumeID := range envelope.WriteSet {
		writeSet[volumeID] = true
	}
	guardIDs := make([]string, 0, len(envelope.guards))
	for guardID := range envelope.guards {
		if !writeSet[guardID] {
			guardIDs = append(guardIDs, guardID)
		}
	}
	sort.Strings(guardIDs)
	for _, guardID := range guardIDs {
		guard := envelope.guards[guardID]
		if guard.Path == "" {
			return guardID, fmt.Errorf("guard path is empty")
		}
		target := filepath.Join(root, filepath.FromSlash(guard.Path))
		info, err := os.Lstat(target)
		if err != nil || !info.Mode().IsRegular() {
			return guardID, err
		}
		fingerprint, err := baseline.HashFile(target)
		if err != nil || fingerprint.SHA256 != guard.SHA256 {
			return guardID, err
		}
	}
	return "", nil
}
