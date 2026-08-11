package onboarding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// RouteVersion is the stable read-only projection returned while a Fresh
// Onboarding Session still owns control, including the narrow crash window in
// which its Bootstrap transaction has published the formal Root but has not
// yet persisted the terminal Session projection.
const RouteVersion = machinecontract.CognitionOnboardingRouteV1

var (
	ErrRouteRecoveryInspection = errors.New("onboarding_route_recovery_inspection_failed")
	ErrRouteRecoveryPending    = errors.New("onboarding_route_recovery_pending")
)

// Route reports only persisted Session and runtime navigation facts. It never
// reads source Evidence, advances authoring, or mutates the Session.
type Route struct {
	Version               string              `json:"version"`
	Status                string              `json:"status"`
	FormalIndexAvailable  bool                `json:"formal_index_available"`
	OnboardingSessionID   string              `json:"onboarding_session_id"`
	SessionVersion        string              `json:"session_version"`
	SessionRevision       int                 `json:"session_revision"`
	Operation             string              `json:"operation"`
	LastSuccessPoint      string              `json:"last_success_point"`
	SessionNextAction     string              `json:"session_next_action"`
	ActiveBatchID         string              `json:"active_batch_id,omitempty"`
	CompletedCount        int                 `json:"completed_count"`
	PendingCount          int                 `json:"pending_count"`
	TransactionState      string              `json:"transaction_state"`
	FormalWritesStarted   bool                `json:"formal_writes_started"`
	RecoveryPending       bool                `json:"recovery_pending"`
	RuntimeRepositoryRoot string              `json:"runtime_repository_root"`
	NextActionContract    *NextActionContract `json:"next_action_contract"`
}

// InspectActiveFreshRoute returns a Route only for a valid, in-progress Fresh
// v2 Bootstrap. Before formal publication the active Session always owns the
// route. After the Root exists, only an apply_pending Session retains control;
// ordinary or completed Sessions never shadow formal cognition. Recovery has
// higher priority and is returned as an error rather than being hidden by the
// onboarding route. executable is the exact currently running AOCI binary
// path.
func InspectActiveFreshRoute(root, indexPath, executable string) (*Route, bool, error) {
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return nil, false, ErrRouteRecoveryInspection
	}
	if len(pending) != 0 {
		return nil, false, ErrRouteRecoveryPending
	}

	session, exists, err := Load(root)
	if err != nil {
		return nil, false, err
	}
	if !exists || session.Version != SessionVersion ||
		session.Operation != cognitionplanOperationBootstrap ||
		session.CurrentLayout != machinecontract.CognitionPlannerUninitialized ||
		session.Status != "in_progress" {
		return nil, false, nil
	}

	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(root, filepath.FromSlash(indexPath))
	}
	formalRootAvailable := false
	if _, err := os.Lstat(indexPath); err == nil {
		formalRootAvailable = true
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("onboarding_route_root_inspection_failed")
	}
	if formalRootAvailable && session.TransactionState != "apply_pending" {
		return nil, false, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, false, fmt.Errorf("onboarding_route_repository_root_invalid")
	}
	absExecutable, err := filepath.Abs(executable)
	if err != nil || executable == "" {
		return nil, false, fmt.Errorf("onboarding_route_executable_invalid")
	}

	batchID := ""
	if session.ActiveAuthoringBatch != nil {
		batchID = session.ActiveAuthoringBatch.BatchID
	}
	nextAction := BuildOnboardingNextActionContract(absRoot, absExecutable, session)
	if session.TransactionState == "apply_pending" && nextAction != nil {
		nextAction.Action = "resume"
		nextAction.Command = BuildHostCommand(absExecutable, []string{
			"--repo", absRoot, "cognition", "onboard", "resume", "--json",
		}, "")
		nextAction.SuccessNextAction = "none"
	}
	if nextAction != nil {
		nextAction.FormalWritesStarted = formalRootAvailable
	}
	route := &Route{
		Version:               RouteVersion,
		Status:                "onboarding_in_progress",
		FormalIndexAvailable:  formalRootAvailable,
		OnboardingSessionID:   session.OnboardingSessionID,
		SessionVersion:        session.Version,
		SessionRevision:       session.Revision,
		Operation:             session.Operation,
		LastSuccessPoint:      session.LastSuccessPoint,
		SessionNextAction:     session.NextAction,
		ActiveBatchID:         batchID,
		CompletedCount:        len(session.CompletedAuthoringTargets),
		PendingCount:          len(session.PendingAuthoringTargets),
		TransactionState:      session.TransactionState,
		FormalWritesStarted:   formalRootAvailable,
		RecoveryPending:       false,
		RuntimeRepositoryRoot: absRoot,
		NextActionContract:    nextAction,
	}
	return route, true, nil
}
