// aoci index relocate: rewrite an index's recorded section roots to the
// neutral root and name it in the Meta, as one receipted transaction.
//
// Every reader already resolved a machine-path section root by relocation,
// so the command changes what a person or a model reads from the file, not
// what the machine does: a committed index then names no author directory,
// and the Meta line explains the token to a reader that has only the bytes.
// The transaction is the Locale switch's shape over different participants:
// the Meta when the line is added, the Code Volume when a header changes, and
// the Baseline when it records either, each published against its exact
// preimage with the Root and every other Volume as unchanged guards; a crash
// leaves relocate-<id>.json under .aoci/transactions that running the same
// command again completes.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

const indexRelocateOperation = "relocate"

// indexRelocateFault is the fault-injection hook the tests use to stop the
// transaction between its steps; production never replaces it.
var indexRelocateFault = func(string) error { return nil }

var (
	relocateTransactionIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)
	relocateSHA256Pattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type indexRelocateImage struct {
	Path            string `json:"path"`
	PreimageSHA256  string `json:"preimage_sha256"`
	PostimageSHA256 string `json:"postimage_sha256"`
}

type indexRelocateGuard struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// indexRelocateIntent is the immutable receipt; see localeSwitchIntent.
type indexRelocateIntent struct {
	Version           int                            `json:"version"`
	Operation         string                         `json:"operation"`
	TransactionID     string                         `json:"transaction_id"`
	SectionsRewritten int                            `json:"sections_rewritten"`
	MetaNoteAdded     bool                           `json:"meta_note_added"`
	Images            []indexRelocateImage           `json:"images"`
	Guards            []indexRelocateGuard           `json:"guards"`
	Staging           []cognitiontxn.StagedPostimage `json:"staging"`
	CreatedAt         string                         `json:"created_at"`
	IntentSHA256      string                         `json:"intent_sha256"`
}

// indexRelocateResult is what the command reports, under --json as is.
type indexRelocateResult struct {
	Outcome           string `json:"outcome"`
	SectionsRewritten int    `json:"sections_rewritten"`
	MetaNoteAdded     bool   `json:"meta_note_added"`
	BaselineAdvanced  bool   `json:"baseline_advanced"`
	TransactionID     string `json:"transaction_id,omitempty"`
}

const (
	indexRelocateApplied   = "applied"
	indexRelocateUnchanged = "unchanged"
	indexRelocateDryRun    = "dry_run"
)

func newIndexRelocateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "relocate",
		Short: indexRelocateShortHelp(),
		Long:  indexRelocateLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}
			result, err := applyIndexRelocate(repoRoot, cfg, dryRun)
			if err != nil {
				var exitErr *ExitError
				if errors.As(err, &exitErr) {
					return exitErr
				}
				if strings.HasPrefix(err.Error(), "index_relocate_") {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("index.relocate.failed", err)}
				}
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if flagJSON {
				return writePlannerJSON(cmd, result)
			}
			if flagQuiet {
				return nil
			}
			out := cmd.OutOrStdout()
			switch result.Outcome {
			case indexRelocateUnchanged:
				_, _ = fmt.Fprintln(out, cliMessage("index.relocate.unchanged"))
			case indexRelocateDryRun:
				if result.SectionsRewritten > 0 {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.dry_run_sections", result.SectionsRewritten))
				}
				if result.MetaNoteAdded {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.dry_run_meta"))
				}
				if result.SectionsRewritten == 0 && !result.MetaNoteAdded {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.dry_run_nothing"))
				}
			default:
				if result.SectionsRewritten > 0 {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.sections", result.SectionsRewritten))
				}
				if result.MetaNoteAdded {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.meta_added"))
				}
				if result.BaselineAdvanced {
					_, _ = fmt.Fprintln(out, cliMessage("index.relocate.baseline_advanced"))
				}
				_, _ = fmt.Fprintln(out, cliMessage("index.relocate.commit_hint"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, cliMessage("index.relocate.flag_dry_run"))
	return cmd
}

// sectionRootNoteFor returns the Meta line that names the neutral root in the
// project Locale, taken from the starter Meta template so an older index gains
// exactly the line a new one is created with.
func sectionRootNoteFor(locale string) (string, error) {
	if !textassets.IsOfficialLocale(locale) {
		locale = textassets.DefaultLocale
	}
	rendered, err := textassets.Render(locale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, index.NeutralRepositoryRoot) {
			return strings.TrimRight(line, "\r"), nil
		}
	}
	return "", errors.New("starter Meta names no section root")
}

// applyIndexRelocate plans and publishes the relocation, or resumes a pending
// one. With dryRun it plans only and reports what would change.
func applyIndexRelocate(repositoryRoot string, cfg *config.Config, dryRun bool) (*indexRelocateResult, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_root_invalid: %w", err)
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_lock_failed: %w", err)
	}
	defer lock.Release()

	pending, err := cognitiontxn.PendingForOperation(root, indexRelocateOperation)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_pending_inspection_failed: %w", err)
	}
	if len(pending) > 1 {
		return nil, errors.New("index_relocate_multiple_receipts")
	}
	if len(pending) == 1 {
		if dryRun {
			return nil, &ExitError{Code: ExitInvalid, MachineCode: "cognition_recovery_pending", Msg: cliMessage("index.relocate.pending_dry_run")}
		}
		intent, loadErr := loadIndexRelocateIntent(indexRelocateIntentPath(root, pending[0]))
		if loadErr != nil {
			return nil, loadErr
		}
		if err := cognitiontxn.RejectOtherPending(root, filepath.Base(indexRelocateIntentPath(root, intent.TransactionID))); err != nil {
			return nil, err
		}
		if err := advanceIndexRelocate(root, cfg, intent); err != nil {
			return nil, err
		}
		return indexRelocateOutcome(intent), nil
	}
	if err := cognitiontxn.RejectOtherPending(root, ""); err != nil {
		return nil, err
	}

	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || len(set.Errors) != 0 {
		return nil, fmt.Errorf("index_relocate_cognition_invalid: %v", err)
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, &ExitError{Code: ExitConfig, MachineCode: machinecontract.StatusLegacyReadOnlyMigrateRequired, Msg: cliMessage("index.relocate.legacy")}
	}
	if set.Meta.State != cognition.AssetPresent || set.Meta.Descriptor.Path == "" {
		return nil, errors.New("index_relocate_meta_missing")
	}
	postimages := map[string][]byte{}
	images := []indexRelocateImage{}

	note, err := sectionRootNoteFor(cfg.Locale)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_note_unavailable: %w", err)
	}
	metaPath := set.Meta.Descriptor.Path
	metaPost, noteAdded := index.EnsureSectionRootNote(set.Meta.Raw, note)
	if noteAdded {
		postimages[metaPath] = metaPost
		images = append(images, indexRelocateImage{Path: metaPath,
			PreimageSHA256: cognitiontxn.SHA256(set.Meta.Raw), PostimageSHA256: cognitiontxn.SHA256(metaPost)})
	}
	sections := 0
	if asset := set.Volumes[cognition.ScopeCode]; asset != nil && asset.State == cognition.AssetPresent && asset.Descriptor.Path != "" {
		text, changed, relocateErr := index.RelocateSectionRoots(string(asset.Raw), root)
		if relocateErr != nil {
			return nil, fmt.Errorf("index_relocate_sections_unresolved: %w", relocateErr)
		}
		if changed > 0 {
			sections = changed
			postimages[asset.Descriptor.Path] = []byte(text)
			images = append(images, indexRelocateImage{Path: asset.Descriptor.Path,
				PreimageSHA256: cognitiontxn.SHA256(asset.Raw), PostimageSHA256: cognitiontxn.SHA256([]byte(text))})
		}
	}
	if len(images) == 0 {
		return &indexRelocateResult{Outcome: indexRelocateUnchanged}, nil
	}

	// The Baseline participates for every rewritten asset it records; a record
	// that is already stale is somebody else's drift to resolve first.
	paths := config.AOCIPaths(root, cfg.IndexPath)
	baselinePre, baselineExists, err := readOptionalRegularFile(paths.BaselinePath)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_baseline_invalid: %w", err)
	}
	baselineAdvanced := false
	if baselineExists {
		state, exists, loadErr := baseline.Load(root)
		if loadErr != nil || !exists || state == nil {
			return nil, fmt.Errorf("index_relocate_baseline_invalid: %v", loadErr)
		}
		if confirm, _, confirmErr := readOptionalRegularFile(paths.BaselinePath); confirmErr != nil || !bytes.Equal(confirm, baselinePre) {
			return nil, errors.New("index_relocate_baseline_snapshot_changed")
		}
		post := *state
		post.Files = make(map[string]baseline.Fingerprint, len(state.Files))
		for path, fingerprint := range state.Files {
			post.Files[path] = fingerprint
		}
		for _, image := range images {
			record, recorded := state.Files[image.Path]
			if !recorded {
				continue
			}
			if record.SHA256 != image.PreimageSHA256 {
				return nil, &ExitError{Code: ExitConfig, Msg: cliMessage("index.relocate.baseline_stale", image.Path)}
			}
			fingerprint := baseline.HashBytes(image.Path, postimages[image.Path])
			fingerprint.Role = record.Role
			post.Files[image.Path] = fingerprint
			baselineAdvanced = true
		}
		if baselineAdvanced {
			post.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			baselinePost, marshalErr := baseline.MarshalExact(&post)
			if marshalErr != nil {
				return nil, fmt.Errorf("index_relocate_baseline_postimage_invalid: %w", marshalErr)
			}
			postimages[".aoci/baseline.json"] = baselinePost
			images = append(images, indexRelocateImage{Path: ".aoci/baseline.json",
				PreimageSHA256: cognitiontxn.SHA256(baselinePre), PostimageSHA256: cognitiontxn.SHA256(baselinePost)})
		}
	}
	guards := indexRelocateGuards(cfg, set, images)
	if dryRun {
		return &indexRelocateResult{Outcome: indexRelocateDryRun, SectionsRewritten: sections, MetaNoteAdded: noteAdded, BaselineAdvanced: baselineAdvanced}, nil
	}
	posts := make([]cognitiontxn.Postimage, 0, len(images))
	for _, image := range images {
		posts = append(posts, cognitiontxn.Postimage{Path: image.Path, SHA: image.PostimageSHA256, Data: postimages[image.Path]})
	}
	transactionID, err := indexRelocateTransactionID(images, guards)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/transactions/history"); err != nil {
		return nil, fmt.Errorf("index_relocate_runtime_boundary_invalid: %w", err)
	}
	staging, err := cognitiontxn.Stage(root, indexRelocateOperation, transactionID, posts, indexRelocateFault)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_staging_failed: %w", err)
	}
	intent := &indexRelocateIntent{
		Version: 1, Operation: indexRelocateOperation, TransactionID: transactionID,
		SectionsRewritten: sections, MetaNoteAdded: noteAdded,
		Images: images, Guards: guards, Staging: staging,
		CreatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	intent.IntentSHA256, err = indexRelocateIntentIdentity(intent)
	if err != nil {
		return nil, err
	}
	intentBytes, err := marshalIndexRelocateIntent(intent)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(indexRelocateIntentPath(root, transactionID), intentBytes); err != nil {
		return nil, fmt.Errorf("index_relocate_intent_failed: %w", err)
	}
	if err := indexRelocateFault("after_intent"); err != nil {
		return nil, err
	}
	if err := advanceIndexRelocate(root, cfg, intent); err != nil {
		return nil, err
	}
	return indexRelocateOutcome(intent), nil
}

func indexRelocateOutcome(intent *indexRelocateIntent) *indexRelocateResult {
	result := &indexRelocateResult{Outcome: indexRelocateApplied, SectionsRewritten: intent.SectionsRewritten,
		MetaNoteAdded: intent.MetaNoteAdded, TransactionID: intent.TransactionID}
	for _, image := range intent.Images {
		if image.Path == ".aoci/baseline.json" {
			result.BaselineAdvanced = true
		}
	}
	return result
}

// advanceIndexRelocate publishes every participant still at its preimage, in
// Intent order, then proves the postimage, writes the Ledger event, and
// archives the receipt; see advanceLocaleSwitch.
func advanceIndexRelocate(root string, cfg *config.Config, intent *indexRelocateIntent) error {
	if err := validateIndexRelocateIntent(intent); err != nil {
		return err
	}
	intentBytes, err := marshalIndexRelocateIntent(intent)
	if err != nil {
		return err
	}
	if err := validateIndexRelocateGuards(root, intent.Guards); err != nil {
		return err
	}
	seenPreimage := false
	for _, image := range intent.Images {
		state, err := indexRelocateImageState(root, image)
		if err != nil {
			return err
		}
		switch state {
		case cognitiontxn.StatePreimage:
			seenPreimage = true
		case cognitiontxn.StatePostimage:
			if seenPreimage {
				return fmt.Errorf("index_relocate_publish_order_invalid: %s", image.Path)
			}
		default:
			return fmt.Errorf("index_relocate_target_conflict: %s", image.Path)
		}
	}
	for _, image := range intent.Images {
		state, err := indexRelocateImageState(root, image)
		if err != nil {
			return err
		}
		if state == cognitiontxn.StatePostimage {
			continue
		}
		if err := validateIndexRelocateGuards(root, intent.Guards); err != nil {
			return err
		}
		if err := indexRelocateFault("before_publish_" + filepath.Base(image.Path)); err != nil {
			return err
		}
		postimage, err := cognitiontxn.ReadStaged(root, intent.Staging, image.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(image.Path))
		if err := afs.AtomicWriteCAS(target, postimage, image.PreimageSHA256); err != nil {
			return fmt.Errorf("index_relocate_publish_failed[%s]: %w", image.Path, err)
		}
		if err := indexRelocateFault("after_publish_" + filepath.Base(image.Path)); err != nil {
			return err
		}
	}
	if err := verifyIndexRelocatePostimage(root, cfg, intent); err != nil {
		return err
	}
	if err := indexRelocateFault("before_ledger"); err != nil {
		return err
	}
	if cfg.LedgerEnabled {
		event := ledger.Event{Op: "index_relocate", Source: "cli", Result: ledger.ResultOK,
			RecoveryTransactionID: intent.TransactionID, AppliedCount: len(intent.Images),
			PathsCount: intent.SectionsRewritten}
		for _, image := range intent.Images {
			switch {
			case image.Path == ".aoci/baseline.json":
				event.BaselineSHA256 = image.PostimageSHA256
			case event.PreIndexSHA256 == "":
				event.PreIndexSHA256, event.PostIndexSHA256, event.IndexSHA256 = image.PreimageSHA256, image.PostimageSHA256, image.PostimageSHA256
			}
		}
		if err := cognitiontxn.EnsureLedger(root, true, event); err != nil {
			return fmt.Errorf("index_relocate_ledger_failed: %w", err)
		}
	}
	if err := indexRelocateFault("before_archive"); err != nil {
		return err
	}
	if err := cognitiontxn.ArchiveImmutable(
		indexRelocateIntentPath(root, intent.TransactionID),
		indexRelocateArchivePath(root, intent.TransactionID),
		intentBytes,
	); err != nil {
		return fmt.Errorf("index_relocate_archive_failed: %w", err)
	}
	return nil
}

func indexRelocateImageState(root string, image indexRelocateImage) (string, error) {
	state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)),
		image.PreimageSHA256, image.PostimageSHA256, false)
	if err != nil {
		return "", fmt.Errorf("index_relocate_target_unreadable[%s]: %w", image.Path, err)
	}
	return state, nil
}

// verifyIndexRelocatePostimage proves the published state from the files:
// every participant at its postimage, the guards unchanged, the layout loading
// as a coherent CognitionSet whose section roots are all neutral and whose
// Meta names the root, and the Baseline recording the rewritten bytes.
func verifyIndexRelocatePostimage(root string, cfg *config.Config, intent *indexRelocateIntent) error {
	for _, image := range intent.Images {
		state, err := indexRelocateImageState(root, image)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("index_relocate_postimage_incomplete: %s", image.Path)
		}
	}
	if err := validateIndexRelocateGuards(root, intent.Guards); err != nil {
		return err
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || len(set.Errors) != 0 || set.LayoutMode != cognition.LayoutVolumesV1 {
		return fmt.Errorf("index_relocate_volume_verification_failed: %v", err)
	}
	if !bytes.Contains(set.Meta.Raw, []byte(index.NeutralRepositoryRoot)) {
		return errors.New("index_relocate_meta_verification_failed")
	}
	if asset := set.Volumes[cognition.ScopeCode]; asset != nil && asset.State == cognition.AssetPresent {
		if _, changed, relocateErr := index.RelocateSectionRoots(string(asset.Raw), root); relocateErr != nil || changed != 0 {
			return errors.New("index_relocate_sections_verification_failed")
		}
	}
	for _, image := range intent.Images {
		if image.Path != ".aoci/baseline.json" {
			continue
		}
		state, exists, loadErr := baseline.Load(root)
		if loadErr != nil || !exists {
			return errors.New("index_relocate_baseline_verification_failed")
		}
		for _, other := range intent.Images {
			if other.Path == ".aoci/baseline.json" {
				continue
			}
			if record, recorded := state.Files[other.Path]; recorded && record.SHA256 != other.PostimageSHA256 {
				return errors.New("index_relocate_baseline_verification_failed")
			}
		}
	}
	return nil
}

// indexRelocateGuards binds every present formal asset the transaction does
// not write: the Root always, and each Volume that stays as it is.
func indexRelocateGuards(cfg *config.Config, set *cognition.Set, images []indexRelocateImage) []indexRelocateGuard {
	written := map[string]bool{}
	for _, image := range images {
		written[image.Path] = true
	}
	guards := []indexRelocateGuard{}
	seen := map[string]bool{}
	add := func(path, sha string) {
		if path == "" || sha == "" || written[path] || seen[path] {
			return
		}
		seen[path] = true
		guards = append(guards, indexRelocateGuard{Path: path, SHA256: sha})
	}
	add(cfg.IndexPath, set.Root.SHA256)
	if set.Meta.State == cognition.AssetPresent {
		add(set.Meta.Descriptor.Path, set.Meta.SHA256)
	}
	for _, id := range set.DeclaredOrder {
		if asset := set.Volumes[id]; asset != nil && asset.State == cognition.AssetPresent {
			add(asset.Descriptor.Path, asset.SHA256)
		}
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].Path < guards[j].Path })
	return guards
}

func validateIndexRelocateGuards(root string, guards []indexRelocateGuard) error {
	for _, guard := range guards {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(guard.Path)), "", guard.SHA256, true)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("index_relocate_guard_drift: %s", guard.Path)
		}
	}
	return nil
}

func indexRelocateTransactionID(images []indexRelocateImage, guards []indexRelocateGuard) (string, error) {
	identity, err := json.Marshal(struct {
		Operation string               `json:"operation"`
		Images    []indexRelocateImage `json:"images"`
		Guards    []indexRelocateGuard `json:"guards"`
	}{indexRelocateOperation, images, guards})
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(identity)[:24], nil
}

func indexRelocateIntentIdentity(intent *indexRelocateIntent) (string, error) {
	clone := *intent
	clone.IntentSHA256 = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(data), nil
}

func marshalIndexRelocateIntent(intent *indexRelocateIntent) ([]byte, error) {
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// validateIndexRelocateIntent re-derives everything the Intent claims about
// itself; a receipt that fails stays pending for a person to inspect.
func validateIndexRelocateIntent(intent *indexRelocateIntent) error {
	if intent == nil || intent.Version != 1 || intent.Operation != indexRelocateOperation ||
		!relocateTransactionIDPattern.MatchString(intent.TransactionID) ||
		len(intent.Images) < 1 || len(intent.Images) > 3 || len(intent.Staging) != len(intent.Images) ||
		intent.SectionsRewritten < 0 || (intent.SectionsRewritten == 0 && !intent.MetaNoteAdded) {
		return errors.New("index_relocate_intent_invalid")
	}
	for position, image := range intent.Images {
		normalized, normalizeErr := afs.NormalizeRelPath(image.Path)
		wantStaging := filepath.ToSlash(filepath.Join(
			".aoci", "transactions", indexRelocateOperation+"-"+intent.TransactionID,
			"staging", fmt.Sprintf("%02d.post", position),
		))
		if normalizeErr != nil || normalized != image.Path ||
			(image.Path == ".aoci/baseline.json") != (position == len(intent.Images)-1 && len(intent.Images) > 1) && image.Path == ".aoci/baseline.json" ||
			!relocateSHA256Pattern.MatchString(image.PostimageSHA256) || !relocateSHA256Pattern.MatchString(image.PreimageSHA256) ||
			intent.Staging[position].Path != image.Path || intent.Staging[position].SHA256 != image.PostimageSHA256 ||
			intent.Staging[position].StagingRel != wantStaging || intent.Staging[position].ByteSize < 0 {
			return fmt.Errorf("index_relocate_image_invalid: %s", image.Path)
		}
	}
	previous := ""
	for position, guard := range intent.Guards {
		normalized, normalizeErr := afs.NormalizeRelPath(guard.Path)
		if normalizeErr != nil || normalized != guard.Path || !relocateSHA256Pattern.MatchString(guard.SHA256) ||
			(position > 0 && guard.Path <= previous) {
			return fmt.Errorf("index_relocate_guard_invalid: %s", guard.Path)
		}
		for _, image := range intent.Images {
			if filepath.ToSlash(image.Path) == guard.Path {
				return fmt.Errorf("index_relocate_guard_invalid: %s", guard.Path)
			}
		}
		previous = guard.Path
	}
	transactionID, err := indexRelocateTransactionID(intent.Images, intent.Guards)
	if err != nil || transactionID != intent.TransactionID {
		return errors.New("index_relocate_transaction_identity_invalid")
	}
	want, err := indexRelocateIntentIdentity(intent)
	if err != nil || want != intent.IntentSHA256 {
		return errors.New("index_relocate_intent_identity_invalid")
	}
	return nil
}

func loadIndexRelocateIntent(path string) (*indexRelocateIntent, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, fmt.Errorf("index_relocate_intent_read_failed: %w", err)
	}
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("index_relocate_intent_invalid: %w", err)
	}
	var intent indexRelocateIntent
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, fmt.Errorf("index_relocate_intent_invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("index_relocate_intent_invalid: trailing JSON")
	}
	if err := validateIndexRelocateIntent(&intent); err != nil {
		return nil, err
	}
	canonical, err := marshalIndexRelocateIntent(&intent)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, errors.New("index_relocate_intent_invalid: noncanonical bytes")
	}
	return &intent, nil
}

func indexRelocateIntentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", indexRelocateOperation+"-"+transactionID+".json")
}

func indexRelocateArchivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", indexRelocateOperation+"-"+transactionID+".json")
}
