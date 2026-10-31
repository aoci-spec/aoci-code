package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// The Locale switch of a Volumes v1 repository.
//
// `config set locale` used to be refused once a Volumes index existed: the
// Legacy pipeline that re-authored a whole index in the new language was the
// only switch there was, and Volumes never had one, so a repository created in
// one Locale could not move to the other without being deleted and rebuilt.
// Ported from dignfei's #28 (tracked in #34) onto the Volumes layout: the
// switch replaces only the Root's machine-readable #Locale line, the team
// configuration's `locale`, and the Baseline's record of the Root, as one
// resumable CAS transaction under the index lock. Meta, Code, and Database
// Volume bytes are guards, never targets, so no Entry is rewritten and no
// authoring debt is created; new or updated Entries are authored in the new
// Locale from then on. A crash leaves `locale-<id>.json` under
// .aoci/transactions, the same command with the same target resumes it, and
// every other command waits.

const localeSwitchOperation = "locale"

// localeSwitchFault is the fault-injection hook the tests use to stop the
// transaction between its steps; production never replaces it.
var localeSwitchFault = func(string) error { return nil }

var (
	localeTransactionIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)
	localeSHA256Pattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// localeSwitchImage is one published participant: its path and the exact
// preimage and postimage digests.
type localeSwitchImage struct {
	Path            string `json:"path"`
	PreimageSHA256  string `json:"preimage_sha256"`
	PostimageSHA256 string `json:"postimage_sha256"`
}

// localeSwitchGuard is a formal asset the switch never writes but must find
// unchanged before every publish: the present Meta, Code, and Database
// Volumes. Their drift during the switch means another writer is active.
type localeSwitchGuard struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// localeSwitchIntent is the immutable receipt. Its bytes are bound by
// IntentSHA256 and its identity by TransactionID; a resume re-derives both and
// refuses a receipt that no longer matches what it claims.
type localeSwitchIntent struct {
	Version       int                            `json:"version"`
	Operation     string                         `json:"operation"`
	TransactionID string                         `json:"transaction_id"`
	TargetLocale  string                         `json:"target_locale"`
	Images        []localeSwitchImage            `json:"images"`
	Guards        []localeSwitchGuard            `json:"guards"`
	Staging       []cognitiontxn.StagedPostimage `json:"staging"`
	CreatedAt     string                         `json:"created_at"`
	IntentSHA256  string                         `json:"intent_sha256"`
}

// localeSwitchOutcome tells the command what the switch did so its notices
// name only what happened.
type localeSwitchOutcome string

const (
	// localeSwitchConfigOnly: no index yet, or a Legacy index that keeps its
	// Locale; only the team configuration changed, as before.
	localeSwitchConfigOnly localeSwitchOutcome = "config_only"
	// localeSwitchUnchanged: Root and configuration already said the target.
	localeSwitchUnchanged localeSwitchOutcome = "unchanged"
	// localeSwitchApplied: the transaction published, or resumed to, its
	// postimage.
	localeSwitchApplied localeSwitchOutcome = "applied"
)

// applyLocaleSwitch changes the project Locale to target. On a Volumes v1
// repository it is the transaction described above; a repository without an
// index or with a Legacy index keeps the configuration-only behavior of
// prepareLocaleChange. Re-running the command with the same target is the
// recovery entry point for a pending receipt.
func applyLocaleSwitch(repositoryRoot, target string) (*config.Config, localeSwitchOutcome, error) {
	if !textassets.IsOfficialLocale(target) {
		return nil, "", errors.New(cliMessage("config.unsupported_locale", target))
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_root_invalid: %w", err)
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_lock_failed: %w", err)
	}
	defer lock.Release()

	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_pending_inspection_failed: %w", err)
	}
	if len(pending) > 1 {
		return nil, "", errors.New("locale_switch_multiple_receipts")
	}
	if len(pending) == 1 {
		intent, loadErr := loadLocaleSwitchIntent(localeSwitchIntentPath(root, pending[0]))
		if loadErr != nil {
			return nil, "", loadErr
		}
		if intent.TargetLocale != target {
			return nil, "", errors.New(cliMessage("locale.switch_target_conflict", intent.TargetLocale, intent.TargetLocale))
		}
		if err := cognitiontxn.RejectOtherPending(root, filepath.Base(localeSwitchIntentPath(root, intent.TransactionID))); err != nil {
			return nil, "", err
		}
		if err := advanceLocaleSwitch(root, intent); err != nil {
			return nil, "", err
		}
		cfg, loadErr := config.LoadBase(root)
		if loadErr != nil {
			return nil, "", loadErr
		}
		return cfg, localeSwitchApplied, nil
	}
	if err := cognitiontxn.RejectOtherPending(root, ""); err != nil {
		return nil, "", err
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		return nil, "", err
	}
	if cfg.LocaleMigration != nil && !(cfg.LocaleMigration.ToLocale == target && cfg.Locale == target) {
		return nil, "", errors.New(cliMessage("locale.migration_in_progress", cfg.LocaleMigration.FromLocale, cfg.LocaleMigration.ToLocale))
	}
	indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
	indexPre, indexExists, err := readOptionalRegularFile(indexPath)
	if err != nil {
		return nil, "", errors.New(cliMessage("locale.read_index_error", err))
	}
	layout := ""
	if indexExists {
		if layout, err = cognition.DetectLayout(indexPre); err != nil {
			return nil, "", errors.New(cliMessage("locale.detect_index_error", err))
		}
	}
	if !indexExists || layout != cognition.LayoutVolumesV1 {
		// The configuration-only paths: a repository without an index takes
		// the Locale; a Legacy index keeps the one it was written in.
		if err := prepareLocaleChange(root, cfg, target); err != nil {
			return nil, "", err
		}
		if err := config.Save(root, cfg); err != nil {
			return nil, "", errors.New(cliMessage("config.write_error", err))
		}
		return cfg, localeSwitchConfigOnly, nil
	}

	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || len(set.Errors) != 0 {
		return nil, "", fmt.Errorf("locale_switch_cognition_invalid: %v", err)
	}
	if !bytes.Equal(set.Root.Raw, indexPre) {
		return nil, "", errors.New("locale_switch_cognition_snapshot_changed")
	}
	indexPost, err := index.ReplaceLocaleMarker(indexPre, target)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_marker_invalid: %w", err)
	}
	// LoadBase materialized the team file if it was absent (from the Root's
	// own #Locale marker), so the configuration preimage is always a file.
	configPath := config.FilePath(root)
	configPre, err := readRegularFile(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_config_preimage_invalid: %w", err)
	}
	cfg.Locale = target
	cfg.LocaleMigration = nil
	configPost, err := config.MarshalBase(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_config_postimage_invalid: %w", err)
	}
	if bytes.Equal(indexPre, indexPost) && bytes.Equal(configPre, configPost) {
		return cfg, localeSwitchUnchanged, nil
	}

	images := []localeSwitchImage{
		{Path: cfg.IndexPath, PreimageSHA256: cognitiontxn.SHA256(indexPre), PostimageSHA256: cognitiontxn.SHA256(indexPost)},
		{Path: ".aoci/config.json", PreimageSHA256: cognitiontxn.SHA256(configPre), PostimageSHA256: cognitiontxn.SHA256(configPost)},
	}
	posts := []cognitiontxn.Postimage{
		{Path: cfg.IndexPath, SHA: images[0].PostimageSHA256, Data: indexPost},
		{Path: ".aoci/config.json", SHA: images[1].PostimageSHA256, Data: configPost},
	}
	// The Baseline participates only when it records the Root: an unbound Root
	// is a legal layout (Guide and Scope skip it too), and a bound Root whose
	// record is already stale is somebody else's drift to resolve first, not
	// something this switch may paper over by rebinding it.
	paths := config.AOCIPaths(root, cfg.IndexPath)
	baselinePre, baselineExists, err := readOptionalRegularFile(paths.BaselinePath)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_baseline_invalid: %w", err)
	}
	if baselineExists && !bytes.Equal(indexPre, indexPost) {
		baselineValue, exists, loadErr := baseline.Load(root)
		if loadErr != nil || !exists || baselineValue == nil {
			return nil, "", fmt.Errorf("locale_switch_baseline_invalid: %v", loadErr)
		}
		if baselineConfirm, _, confirmErr := readOptionalRegularFile(paths.BaselinePath); confirmErr != nil || !bytes.Equal(baselineConfirm, baselinePre) {
			return nil, "", errors.New("locale_switch_baseline_snapshot_changed")
		}
		if record, recorded := baselineValue.Files[cfg.IndexPath]; recorded {
			if record.SHA256 != images[0].PreimageSHA256 {
				return nil, "", errors.New(cliMessage("locale.baseline_root_stale", cfg.IndexPath))
			}
			post := *baselineValue
			post.Files = make(map[string]baseline.Fingerprint, len(baselineValue.Files))
			for path, fingerprint := range baselineValue.Files {
				post.Files[path] = fingerprint
			}
			fingerprint := baseline.HashBytes(cfg.IndexPath, indexPost)
			fingerprint.Role = record.Role
			post.Files[cfg.IndexPath] = fingerprint
			post.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			baselinePost, marshalErr := baseline.MarshalExact(&post)
			if marshalErr != nil {
				return nil, "", fmt.Errorf("locale_switch_baseline_postimage_invalid: %w", marshalErr)
			}
			images = append(images, localeSwitchImage{Path: ".aoci/baseline.json",
				PreimageSHA256: cognitiontxn.SHA256(baselinePre), PostimageSHA256: cognitiontxn.SHA256(baselinePost)})
			posts = append(posts, cognitiontxn.Postimage{Path: ".aoci/baseline.json", SHA: cognitiontxn.SHA256(baselinePost), Data: baselinePost})
		}
	}
	guards := localeSwitchGuards(set)
	transactionID, err := localeSwitchTransactionID(target, images, guards)
	if err != nil {
		return nil, "", err
	}
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/transactions/history"); err != nil {
		return nil, "", fmt.Errorf("locale_switch_runtime_boundary_invalid: %w", err)
	}
	staging, err := cognitiontxn.Stage(root, localeSwitchOperation, transactionID, posts, localeSwitchFault)
	if err != nil {
		return nil, "", fmt.Errorf("locale_switch_staging_failed: %w", err)
	}
	intent := &localeSwitchIntent{
		Version: 1, Operation: localeSwitchOperation, TransactionID: transactionID,
		TargetLocale: target, Images: images, Guards: guards, Staging: staging,
		CreatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	intent.IntentSHA256, err = localeSwitchIntentIdentity(intent)
	if err != nil {
		return nil, "", err
	}
	intentBytes, err := marshalLocaleSwitchIntent(intent)
	if err != nil {
		return nil, "", err
	}
	if err := cognitiontxn.SaveImmutable(localeSwitchIntentPath(root, transactionID), intentBytes); err != nil {
		return nil, "", fmt.Errorf("locale_switch_intent_failed: %w", err)
	}
	if err := localeSwitchFault("after_intent"); err != nil {
		return nil, "", err
	}
	if err := advanceLocaleSwitch(root, intent); err != nil {
		return nil, "", err
	}
	committed, err := config.LoadBase(root)
	if err != nil {
		return nil, "", err
	}
	return committed, localeSwitchApplied, nil
}

// advanceLocaleSwitch publishes every participant that still shows its
// preimage, in Intent order, and finishes with the postimage proof, the Ledger
// event, and the archive. It is idempotent: a resume after any crash point
// skips what is already published and refuses what is neither preimage nor
// postimage.
func advanceLocaleSwitch(root string, intent *localeSwitchIntent) error {
	if err := validateLocaleSwitchIntent(intent); err != nil {
		return err
	}
	intentBytes, err := marshalLocaleSwitchIntent(intent)
	if err != nil {
		return err
	}
	if err := validateLocaleSwitchGuards(root, intent.Guards); err != nil {
		return err
	}
	seenPreimage := false
	for _, image := range intent.Images {
		state, err := localeSwitchImageState(root, image)
		if err != nil {
			return err
		}
		switch state {
		case cognitiontxn.StatePreimage:
			seenPreimage = true
		case cognitiontxn.StatePostimage:
			if seenPreimage {
				return fmt.Errorf("locale_switch_publish_order_invalid: %s", image.Path)
			}
		default:
			return fmt.Errorf("locale_switch_target_conflict: %s", image.Path)
		}
	}
	for _, image := range intent.Images {
		state, err := localeSwitchImageState(root, image)
		if err != nil {
			return err
		}
		if state == cognitiontxn.StatePostimage {
			continue
		}
		if err := validateLocaleSwitchGuards(root, intent.Guards); err != nil {
			return err
		}
		if err := localeSwitchFault("before_publish_" + filepath.Base(image.Path)); err != nil {
			return err
		}
		postimage, err := cognitiontxn.ReadStaged(root, intent.Staging, image.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(image.Path))
		if err := afs.AtomicWriteCAS(target, postimage, image.PreimageSHA256); err != nil {
			return fmt.Errorf("locale_switch_publish_failed[%s]: %w", image.Path, err)
		}
		if err := localeSwitchFault("after_publish_" + filepath.Base(image.Path)); err != nil {
			return err
		}
	}
	cfg, err := verifyLocaleSwitchPostimage(root, intent)
	if err != nil {
		return err
	}
	if err := localeSwitchFault("before_ledger"); err != nil {
		return err
	}
	if cfg.LedgerEnabled {
		event := ledger.Event{Op: "locale_switch_apply", Source: "cli", Result: ledger.ResultOK,
			RecoveryTransactionID: intent.TransactionID, AppliedCount: len(intent.Images),
			PreIndexSHA256: intent.Images[0].PreimageSHA256, PostIndexSHA256: intent.Images[0].PostimageSHA256,
			IndexSHA256: intent.Images[0].PostimageSHA256}
		for _, image := range intent.Images {
			if image.Path == ".aoci/baseline.json" {
				event.BaselineSHA256 = image.PostimageSHA256
			}
		}
		if err := cognitiontxn.EnsureLedger(root, true, event); err != nil {
			return fmt.Errorf("locale_switch_ledger_failed: %w", err)
		}
	}
	if err := localeSwitchFault("before_archive"); err != nil {
		return err
	}
	if err := cognitiontxn.ArchiveImmutable(
		localeSwitchIntentPath(root, intent.TransactionID),
		localeSwitchArchivePath(root, intent.TransactionID),
		intentBytes,
	); err != nil {
		return fmt.Errorf("locale_switch_archive_failed: %w", err)
	}
	return nil
}

func localeSwitchImageState(root string, image localeSwitchImage) (string, error) {
	state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)),
		image.PreimageSHA256, image.PostimageSHA256, false)
	if err != nil {
		return "", fmt.Errorf("locale_switch_target_unreadable[%s]: %w", image.Path, err)
	}
	return state, nil
}

// verifyLocaleSwitchPostimage proves the published state from the files
// themselves: every participant at its postimage, the configuration and the
// Root naming the target, the Baseline record matching the Root when it
// participated, and the layout loading as a coherent CognitionSet.
func verifyLocaleSwitchPostimage(root string, intent *localeSwitchIntent) (*config.Config, error) {
	for _, image := range intent.Images {
		state, err := localeSwitchImageState(root, image)
		if err != nil || state != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("locale_switch_postimage_incomplete: %s", image.Path)
		}
	}
	if err := validateLocaleSwitchGuards(root, intent.Guards); err != nil {
		return nil, err
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != intent.TargetLocale || cfg.LocaleMigration != nil {
		return nil, errors.New("locale_switch_config_verification_failed")
	}
	indexRaw, err := readRegularFile(filepath.Join(root, filepath.FromSlash(cfg.IndexPath)))
	if err != nil {
		return nil, fmt.Errorf("locale_switch_marker_verification_failed: %w", err)
	}
	locale, explicit, err := index.DetectLocale(string(indexRaw))
	if err != nil || !explicit || locale != intent.TargetLocale {
		return nil, errors.New("locale_switch_marker_verification_failed")
	}
	for _, image := range intent.Images {
		if image.Path != ".aoci/baseline.json" {
			continue
		}
		baselineValue, exists, loadErr := baseline.Load(root)
		if loadErr != nil || !exists || baselineValue.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(indexRaw) {
			return nil, errors.New("locale_switch_baseline_verification_failed")
		}
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || len(set.Errors) != 0 {
		return nil, fmt.Errorf("locale_switch_volume_verification_failed: %v", err)
	}
	return cfg, nil
}

// localeSwitchGuards binds every present Volume other than the Root. The Root
// is a participant; the others must not move while it is being rewritten.
func localeSwitchGuards(set *cognition.Set) []localeSwitchGuard {
	guards := []localeSwitchGuard{}
	if set == nil {
		return guards
	}
	seen := map[string]bool{}
	add := func(asset *cognition.Asset) {
		if asset == nil || asset.State != cognition.AssetPresent || asset.Descriptor.Path == "" || seen[asset.Descriptor.Path] {
			return
		}
		seen[asset.Descriptor.Path] = true
		guards = append(guards, localeSwitchGuard{Path: asset.Descriptor.Path, SHA256: asset.SHA256})
	}
	add(&set.Meta)
	for _, id := range set.DeclaredOrder {
		add(set.Volumes[id])
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].Path < guards[j].Path })
	return guards
}

func validateLocaleSwitchGuards(root string, guards []localeSwitchGuard) error {
	for _, guard := range guards {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(guard.Path)), "", guard.SHA256, true)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("locale_switch_guard_drift: %s", guard.Path)
		}
	}
	return nil
}

func localeSwitchTransactionID(target string, images []localeSwitchImage, guards []localeSwitchGuard) (string, error) {
	identity, err := json.Marshal(struct {
		Target string              `json:"target"`
		Images []localeSwitchImage `json:"images"`
		Guards []localeSwitchGuard `json:"guards"`
	}{target, images, guards})
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(identity)[:24], nil
}

func localeSwitchIntentIdentity(intent *localeSwitchIntent) (string, error) {
	clone := *intent
	clone.IntentSHA256 = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(data), nil
}

func marshalLocaleSwitchIntent(intent *localeSwitchIntent) ([]byte, error) {
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// validateLocaleSwitchIntent re-derives everything the Intent claims about
// itself. A receipt that fails here is not resumed and not rolled back by
// guesswork; it stays pending for a person to inspect.
func validateLocaleSwitchIntent(intent *localeSwitchIntent) error {
	if intent == nil || intent.Version != 1 || intent.Operation != localeSwitchOperation ||
		!localeTransactionIDPattern.MatchString(intent.TransactionID) || !textassets.IsOfficialLocale(intent.TargetLocale) ||
		len(intent.Images) < 2 || len(intent.Images) > 3 || intent.Images[0].Path != "aoci.txt" ||
		intent.Images[1].Path != ".aoci/config.json" || (len(intent.Images) == 3 && intent.Images[2].Path != ".aoci/baseline.json") ||
		len(intent.Staging) != len(intent.Images) {
		return errors.New("locale_switch_intent_invalid")
	}
	for position, image := range intent.Images {
		wantStaging := filepath.ToSlash(filepath.Join(
			".aoci", "transactions", localeSwitchOperation+"-"+intent.TransactionID,
			"staging", fmt.Sprintf("%02d.post", position),
		))
		if !localeSHA256Pattern.MatchString(image.PostimageSHA256) || !localeSHA256Pattern.MatchString(image.PreimageSHA256) ||
			intent.Staging[position].Path != image.Path || intent.Staging[position].SHA256 != image.PostimageSHA256 ||
			intent.Staging[position].StagingRel != wantStaging || intent.Staging[position].ByteSize < 0 {
			return fmt.Errorf("locale_switch_image_invalid: %s", image.Path)
		}
	}
	previous := ""
	for position, guard := range intent.Guards {
		normalized, normalizeErr := afs.NormalizeRelPath(guard.Path)
		if normalizeErr != nil || normalized != guard.Path || !localeSHA256Pattern.MatchString(guard.SHA256) ||
			(position > 0 && guard.Path <= previous) {
			return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
		}
		for _, image := range intent.Images {
			if filepath.ToSlash(image.Path) == guard.Path {
				return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
			}
		}
		previous = guard.Path
	}
	transactionID, err := localeSwitchTransactionID(intent.TargetLocale, intent.Images, intent.Guards)
	if err != nil || transactionID != intent.TransactionID {
		return errors.New("locale_switch_transaction_identity_invalid")
	}
	want, err := localeSwitchIntentIdentity(intent)
	if err != nil || want != intent.IntentSHA256 {
		return errors.New("locale_switch_intent_identity_invalid")
	}
	return nil
}

func loadLocaleSwitchIntent(path string) (*localeSwitchIntent, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_intent_read_failed: %w", err)
	}
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("locale_switch_intent_invalid: %w", err)
	}
	var intent localeSwitchIntent
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, fmt.Errorf("locale_switch_intent_invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("locale_switch_intent_invalid: trailing JSON")
	}
	if err := validateLocaleSwitchIntent(&intent); err != nil {
		return nil, err
	}
	canonical, err := marshalLocaleSwitchIntent(&intent)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, errors.New("locale_switch_intent_invalid: noncanonical bytes")
	}
	return &intent, nil
}

func localeSwitchIntentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", localeSwitchOperation+"-"+transactionID+".json")
}

func localeSwitchArchivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", localeSwitchOperation+"-"+transactionID+".json")
}

// readRegularFile reads a path that must be an existing regular file; a
// symlink or a directory where a formal asset should be is refused rather
// than followed.
func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return os.ReadFile(path)
}

func readOptionalRegularFile(path string) ([]byte, bool, error) {
	data, err := readRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}
