package onboarding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func SessionPath(root string) string {
	return filepath.Join(root, ".aoci", "onboarding", "active.json")
}

func artifactPath(root string, session *Session, name string) string {
	return filepath.Join(root, ".aoci", "onboarding", session.OnboardingSessionID, "artifacts", name)
}

func Load(root string) (*Session, bool, error) {
	data, err := os.ReadFile(SessionPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("onboarding_session_read_failed")
	}
	var session Session
	if err := decodeStrict(data, &session); err != nil {
		return nil, true, fmt.Errorf("onboarding_session_invalid: %w", err)
	}
	if err := validateSession(&session); err != nil {
		return nil, true, err
	}
	digest := sha256.Sum256(data)
	session.PreimageSHA256 = hex.EncodeToString(digest[:])
	return &session, true, nil
}

func save(root string, session *Session) error {
	if err := validateSession(session); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(SessionPath(root)), 0o700); err != nil {
		return fmt.Errorf("onboarding_session_directory_failed")
	}
	path := SessionPath(root)
	if session.PreimageSHA256 == "" {
		if err := afs.AtomicCreateCAS(path, data); err != nil {
			return err
		}
	} else if err := afs.AtomicWriteCAS(path, data, session.PreimageSHA256); err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	session.PreimageSHA256 = hex.EncodeToString(digest[:])
	return nil
}

func saveArtifact(root string, session *Session, name string, value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return saveArtifactBytes(root, session, name, append(data, '\n'))
}

func saveArtifactBytes(root string, session *Session, name string, data []byte) (string, error) {
	path := artifactPath(root, session, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return filepath.ToSlash(mustRel(root, path)), nil
		}
		return "", fmt.Errorf("onboarding_artifact_conflict: %s", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		return "", err
	}
	return filepath.ToSlash(mustRel(root, path)), nil
}

func readArtifact(root, relative string) ([]byte, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	base := filepath.Join(root, ".aoci", "onboarding")
	abs := filepath.Join(root, clean)
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return nil, fmt.Errorf("onboarding_artifact_path_invalid")
	}
	return os.ReadFile(abs)
}

func decodeStrict(data []byte, target any) error {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing_json")
	}
	return nil
}

func validateSession(session *Session) error {
	if session == nil || session.Revision < 1 || session.OnboardingSessionID == "" ||
		session.RepositoryIdentity == "" || session.PlanID == "" || session.PlanArtifact == "" || session.FrozenBaselineTimestamp == "" {
		return fmt.Errorf("onboarding_session_identity_invalid")
	}
	if session.Version != LegacySessionVersion && session.Version != SessionVersion {
		return fmt.Errorf("onboarding_session_version_invalid")
	}
	if session.Operation != cognitionplanOperationBootstrap && session.Operation != cognitionplanOperationMigration {
		return fmt.Errorf("onboarding_session_operation_invalid")
	}
	if session.Version == SessionVersion && session.Operation != cognitionplanOperationBootstrap {
		return fmt.Errorf("onboarding_session_version_operation_mismatch")
	}
	if session.Version == LegacySessionVersion && (session.SemanticAuthoringDeclaration != nil || session.ActiveAuthoringBatch != nil) {
		return fmt.Errorf("onboarding_session_cross_version_fields")
	}
	if session.AutomationPolicy != nil {
		mode, err := config.ParseAutomationMode(session.AutomationPolicy.Mode)
		if err != nil || mode != session.AutomationPolicy.Mode || session.AutomationPolicy.Source == "" {
			return fmt.Errorf("onboarding_session_automation_policy_invalid")
		}
		switch session.AutomationPolicy.Source {
		case machinecontract.CognitionAutomationPolicyTeamConfig:
			if mode == config.AutomationModeLegacy {
				return fmt.Errorf("onboarding_session_automation_policy_invalid")
			}
		case machinecontract.CognitionAutomationPolicyFreshDefault:
			if mode != config.AutomationModeAuto || session.Operation != cognitionplanOperationBootstrap {
				return fmt.Errorf("onboarding_session_automation_policy_invalid")
			}
		case machinecontract.CognitionAutomationPolicyLegacy:
			if mode != config.AutomationModeLegacy {
				return fmt.Errorf("onboarding_session_automation_policy_invalid")
			}
		default:
			return fmt.Errorf("onboarding_session_automation_policy_invalid")
		}
	}
	if session.AuthorizationProjection != nil && session.Operation != cognitionplanOperationBootstrap {
		return fmt.Errorf("onboarding_session_authorization_projection_invalid")
	}
	if session.BusinessRowsRead != 0 || session.DDLDMLStatements != 0 || session.NetworkAccessed {
		return fmt.Errorf("onboarding_session_security_invariant_failed")
	}
	if !sort.StringsAreSorted(session.CompletedAuthoringTargets) || !sort.StringsAreSorted(session.PendingAuthoringTargets) {
		return fmt.Errorf("onboarding_session_targets_unsorted")
	}
	if session.ActiveAuthoringBatch != nil {
		if session.ActiveAuthoringBatch.BatchID == "" || len(session.ActiveAuthoringBatch.TaskIDs) == 0 ||
			!sort.StringsAreSorted(session.ActiveAuthoringBatch.TaskIDs) || session.ActiveAuthoringBatch.EvidenceBytes < 0 {
			return fmt.Errorf("onboarding_session_active_batch_invalid")
		}
	}
	return nil
}

const (
	cognitionplanOperationBootstrap = "bootstrap"
	cognitionplanOperationMigration = "migration"
)

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		panic(err)
	}
	return rel
}
