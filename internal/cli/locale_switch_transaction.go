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
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/textassets"
)

const localeSwitchOperation = "locale"

var (
	localeSwitchFault     = func(string) error { return nil }
	localeTransactionIDRE = regexp.MustCompile(`^[a-f0-9]{24}$`)
	localeSHA256RE        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type localeSwitchImage struct {
	Path            string `json:"path"`
	PreimageSHA256  string `json:"preimage_sha256"`
	PostimageSHA256 string `json:"postimage_sha256"`
}

type localeSwitchGuard struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Absent bool   `json:"absent,omitempty"`
}

type localeSwitchIntent struct {
	Version       int                            `json:"version"`
	Operation     string                         `json:"operation"`
	TransactionID string                         `json:"transaction_id"`
	TargetLocale  string                         `json:"target_locale"`
	Layout        string                         `json:"layout"`
	Images        []localeSwitchImage            `json:"images"`
	Guards        []localeSwitchGuard            `json:"guards"`
	Staging       []cognitiontxn.StagedPostimage `json:"staging"`
	CreatedAt     string                         `json:"created_at"`
	IntentSHA256  string                         `json:"intent_sha256"`
}

// applyLocaleSwitch commits the deterministic marker, team configuration, and
// Baseline receipt as one resumable CAS transaction. Re-running the same
// config command is the recovery entry point.
func applyLocaleSwitch(repositoryRoot, target string) (*config.Config, error) {
	if !textassets.IsOfficialLocale(target) {
		return nil, fmt.Errorf("locale_switch_target_invalid")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_root_invalid")
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_lock_failed: %w", err)
	}
	defer lock.Release()
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/transactions"); err != nil {
		return nil, fmt.Errorf("locale_switch_runtime_boundary_invalid: %w", err)
	}

	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_pending_inspection_failed: %w", err)
	}
	if len(pending) > 1 {
		return nil, fmt.Errorf("locale_switch_multiple_recovery_intents")
	}
	if len(pending) == 1 {
		intent, loadErr := loadLocaleSwitchIntent(localeSwitchIntentPath(root, pending[0]))
		if loadErr != nil {
			return nil, loadErr
		}
		if intent.TargetLocale != target {
			return nil, fmt.Errorf("locale_switch_target_conflict: pending=%s requested=%s", intent.TargetLocale, target)
		}
		if err := cognitiontxn.RejectOtherPending(root, filepath.Base(localeSwitchIntentPath(root, intent.TransactionID))); err != nil {
			return nil, err
		}
		if err := advanceLocaleSwitch(root, intent); err != nil {
			return nil, err
		}
		return config.LoadBase(root)
	}
	if err := cognitiontxn.RejectOtherPending(root, ""); err != nil {
		return nil, err
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_config_invalid: %w", err)
	}
	if _, err := validateLocaleSwitchIndexPath(cfg.IndexPath, ""); err != nil {
		return nil, err
	}
	configPath := config.FilePath(root)
	configPre, configExists, err := readOptionalRegularFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_config_preimage_invalid: %w", err)
	}
	configConfirm, configStillExists, err := readOptionalRegularFile(configPath)
	if err != nil || configStillExists != configExists || !bytes.Equal(configConfirm, configPre) {
		return nil, fmt.Errorf("locale_switch_config_snapshot_changed")
	}
	indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
	indexPre, err := readRegularFile(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := prepareLocaleChange(root, cfg, target); err != nil {
				return nil, err
			}
			configPost, marshalErr := config.MarshalBase(cfg)
			if marshalErr != nil {
				return nil, fmt.Errorf("locale_switch_config_postimage_invalid: %w", marshalErr)
			}
			if bytes.Equal(configPre, configPost) {
				return cfg, nil
			}
			if !configExists {
				if err := afs.AtomicCreateCAS(configPath, configPost); err != nil {
					return nil, fmt.Errorf("locale_switch_config_publish_failed: %w", err)
				}
				return cfg, nil
			}
			if err := afs.AtomicWriteCAS(configPath, configPost, cognitiontxn.SHA256(configPre)); err != nil {
				return nil, fmt.Errorf("locale_switch_config_publish_failed: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("locale_switch_index_preimage_invalid: %w", err)
	}
	if !configExists {
		return nil, fmt.Errorf("locale_switch_config_preimage_missing")
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_cognition_invalid: %w", err)
	}
	if !bytes.Equal(set.Root.Raw, indexPre) {
		return nil, fmt.Errorf("locale_switch_cognition_snapshot_changed")
	}
	if err := prepareLocaleChange(root, cfg, target); err != nil {
		return nil, err
	}
	indexPost, err := index.ReplaceLocaleMarker(indexPre, target)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_marker_invalid: %w", err)
	}
	configPost, err := config.MarshalBase(cfg)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_config_postimage_invalid: %w", err)
	}
	if bytes.Equal(indexPre, indexPost) && bytes.Equal(configPre, configPost) {
		return cfg, nil
	}

	paths := config.AOCIPaths(root, cfg.IndexPath)
	baselinePre, err := readRegularFile(paths.BaselinePath)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_baseline_missing_or_invalid: %w", err)
	}
	baselineValue, exists, err := baseline.Load(root)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_baseline_missing_or_invalid: %w", err)
	}
	if !exists || baselineValue == nil {
		return nil, fmt.Errorf("locale_switch_baseline_missing_or_invalid")
	}
	baselineConfirm, err := readRegularFile(paths.BaselinePath)
	if err != nil || !bytes.Equal(baselineConfirm, baselinePre) {
		return nil, fmt.Errorf("locale_switch_baseline_snapshot_changed")
	}
	rootFingerprint, exists := baselineValue.Files[cfg.IndexPath]
	if !exists || rootFingerprint.SHA256 != cognitiontxn.SHA256(indexPre) {
		return nil, fmt.Errorf("locale_switch_baseline_root_stale")
	}
	baselinePost := baselinePre
	if !bytes.Equal(indexPre, indexPost) {
		postBaseline := *baselineValue
		postBaseline.Files = make(map[string]baseline.Fingerprint, len(baselineValue.Files))
		for path, fingerprint := range baselineValue.Files {
			postBaseline.Files[path] = fingerprint
		}
		fingerprint := baseline.HashBytes(cfg.IndexPath, indexPost)
		fingerprint.Role = rootFingerprint.Role
		postBaseline.Files[cfg.IndexPath] = fingerprint
		postBaseline.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		baselinePost, err = baseline.MarshalExact(&postBaseline)
		if err != nil {
			return nil, fmt.Errorf("locale_switch_baseline_postimage_invalid: %w", err)
		}
	}
	images := []localeSwitchImage{
		{Path: cfg.IndexPath, PreimageSHA256: cognitiontxn.SHA256(indexPre), PostimageSHA256: cognitiontxn.SHA256(indexPost)},
		{Path: ".aoci/config.json", PreimageSHA256: cognitiontxn.SHA256(configPre), PostimageSHA256: cognitiontxn.SHA256(configPost)},
		{Path: ".aoci/baseline.json", PreimageSHA256: cognitiontxn.SHA256(baselinePre), PostimageSHA256: cognitiontxn.SHA256(baselinePost)},
	}
	guards := localeSwitchGuards(set)
	if set.LayoutMode == cognition.LayoutLegacyMonolithic {
		curationGuard, guardErr := localeSwitchFileGuard(root, ".aoci/curation.json")
		if guardErr != nil {
			return nil, fmt.Errorf("locale_switch_curation_guard_invalid: %w", guardErr)
		}
		guards = append(guards, curationGuard)
		sort.Slice(guards, func(i, j int) bool { return guards[i].Path < guards[j].Path })
	}
	transactionID, err := localeSwitchTransactionID(target, set.LayoutMode, images, guards)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/transactions/history"); err != nil {
		return nil, fmt.Errorf("locale_switch_runtime_boundary_invalid: %w", err)
	}
	posts := []cognitiontxn.Postimage{
		{Path: cfg.IndexPath, SHA: images[0].PostimageSHA256, Data: indexPost},
		{Path: ".aoci/config.json", SHA: images[1].PostimageSHA256, Data: configPost},
		{Path: ".aoci/baseline.json", SHA: images[2].PostimageSHA256, Data: baselinePost},
	}
	staging, err := cognitiontxn.Stage(root, localeSwitchOperation, transactionID, posts, localeSwitchFault)
	if err != nil {
		return nil, fmt.Errorf("locale_switch_staging_failed: %w", err)
	}
	intent := &localeSwitchIntent{
		Version: 1, Operation: localeSwitchOperation, TransactionID: transactionID,
		TargetLocale: target, Layout: set.LayoutMode, Images: images, Guards: guards,
		Staging: staging, CreatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	intent.IntentSHA256, err = localeSwitchIntentIdentity(intent)
	if err != nil {
		return nil, err
	}
	intentBytes, err := marshalLocaleSwitchIntent(intent)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(localeSwitchIntentPath(root, transactionID), intentBytes); err != nil {
		return nil, fmt.Errorf("locale_switch_intent_failed: %w", err)
	}
	if err := localeSwitchFault("after_intent"); err != nil {
		return nil, err
	}
	if err := advanceLocaleSwitch(root, intent); err != nil {
		return nil, err
	}
	return config.LoadBase(root)
}

func localeSwitchTransactionID(target, layout string, images []localeSwitchImage, guards []localeSwitchGuard) (string, error) {
	identity, err := json.Marshal(struct {
		Target string              `json:"target"`
		Layout string              `json:"layout"`
		Images []localeSwitchImage `json:"images"`
		Guards []localeSwitchGuard `json:"guards"`
	}{target, layout, images, guards})
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(identity)[:24], nil
}

func advanceLocaleSwitch(root string, intent *localeSwitchIntent) error {
	if err := validateLocaleSwitchIntent(intent); err != nil {
		return err
	}
	for _, relative := range []string{
		".aoci/transactions/history",
		filepath.ToSlash(filepath.Join(".aoci", "transactions", localeSwitchOperation+"-"+intent.TransactionID)),
		filepath.ToSlash(filepath.Join(".aoci", "transactions", localeSwitchOperation+"-"+intent.TransactionID, "staging")),
	} {
		if err := cognitiontxn.EnsureSafeDirectory(root, relative); err != nil {
			return fmt.Errorf("locale_switch_runtime_boundary_invalid: %w", err)
		}
	}
	intentBytes, err := marshalLocaleSwitchIntent(intent)
	if err != nil {
		return err
	}
	if err := cognitiontxn.ValidateImmutableTarget(
		localeSwitchArchivePath(root, intent.TransactionID), intentBytes,
	); err != nil {
		return fmt.Errorf("locale_switch_archive_invalid: %w", err)
	}
	if err := validateLocaleSwitchGuards(root, intent.Guards); err != nil {
		return err
	}
	seenPreimage := false
	for _, image := range intent.Images {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)), image.PreimageSHA256, image.PostimageSHA256, false)
		if err != nil {
			return err
		}
		if image.PreimageSHA256 == image.PostimageSHA256 {
			if state != cognitiontxn.StatePostimage {
				return fmt.Errorf("locale_switch_target_conflict: %s", image.Path)
			}
			continue
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
		if image.PreimageSHA256 == image.PostimageSHA256 {
			continue
		}
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)), image.PreimageSHA256, image.PostimageSHA256, false)
		if err != nil {
			return err
		}
		if state == cognitiontxn.StatePostimage {
			continue
		}
		if state != cognitiontxn.StatePreimage {
			return fmt.Errorf("locale_switch_target_conflict: %s", image.Path)
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
		if err := afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(image.Path)), postimage, image.PreimageSHA256); err != nil {
			return fmt.Errorf("locale_switch_publish_failed[%s]: %w", image.Path, err)
		}
		if err := localeSwitchFault("after_publish_" + filepath.Base(image.Path)); err != nil {
			return err
		}
	}
	if err := verifyLocaleSwitchPostimage(root, intent); err != nil {
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

func verifyLocaleSwitchPostimage(root string, intent *localeSwitchIntent) error {
	for _, image := range intent.Images {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)), image.PreimageSHA256, image.PostimageSHA256, false)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("locale_switch_postimage_incomplete: %s", image.Path)
		}
	}
	if err := validateLocaleSwitchGuards(root, intent.Guards); err != nil {
		return err
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != intent.TargetLocale {
		return fmt.Errorf("locale_switch_config_verification_failed")
	}
	indexRaw, err := readRegularFile(filepath.Join(root, filepath.FromSlash(cfg.IndexPath)))
	if err != nil {
		return err
	}
	locale, _, err := index.DetectLocale(string(indexRaw))
	if err != nil || locale != intent.TargetLocale {
		return fmt.Errorf("locale_switch_marker_verification_failed")
	}
	baselineValue, exists, err := baseline.Load(root)
	if err != nil || !exists || baselineValue.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(indexRaw) {
		return fmt.Errorf("locale_switch_baseline_verification_failed")
	}
	if intent.Layout == cognition.LayoutVolumesV1 {
		if _, err := cognition.Load(root, cfg.IndexPath); err != nil {
			return fmt.Errorf("locale_switch_volume_verification_failed: %w", err)
		}
	}
	return nil
}

func localeSwitchGuards(set *cognition.Set) []localeSwitchGuard {
	if set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return []localeSwitchGuard{}
	}
	guards := []localeSwitchGuard{}
	for _, id := range set.DeclaredOrder {
		asset := set.Volumes[id]
		if asset == nil || asset.State != cognition.AssetPresent {
			continue
		}
		guards = append(guards, localeSwitchGuard{Path: asset.Descriptor.Path, SHA256: asset.SHA256})
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].Path < guards[j].Path })
	return guards
}

func validateLocaleSwitchGuards(root string, guards []localeSwitchGuard) error {
	for _, guard := range guards {
		if guard.Absent {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(guard.Path))); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("locale_switch_guard_drift: %s", guard.Path)
			}
			continue
		}
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(guard.Path)), "", guard.SHA256, true)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("locale_switch_guard_drift: %s", guard.Path)
		}
	}
	return nil
}

func localeSwitchFileGuard(root, relativePath string) (localeSwitchGuard, error) {
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return localeSwitchGuard{Path: relativePath, Absent: true}, nil
	} else if err != nil {
		return localeSwitchGuard{}, err
	}
	data, err := readRegularFile(path)
	if err != nil {
		return localeSwitchGuard{}, err
	}
	return localeSwitchGuard{Path: relativePath, SHA256: cognitiontxn.SHA256(data)}, nil
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

func validateLocaleSwitchIntent(intent *localeSwitchIntent) error {
	if intent == nil || intent.Version != 1 || intent.Operation != localeSwitchOperation ||
		!localeTransactionIDRE.MatchString(intent.TransactionID) || !textassets.IsOfficialLocale(intent.TargetLocale) ||
		(intent.Layout != cognition.LayoutLegacyMonolithic && intent.Layout != cognition.LayoutVolumesV1) ||
		len(intent.Images) != 3 || intent.Images[0].Path == "" || intent.Images[1].Path != ".aoci/config.json" ||
		intent.Images[2].Path != ".aoci/baseline.json" || len(intent.Staging) != len(intent.Images) {
		return fmt.Errorf("locale_switch_intent_invalid")
	}
	if _, err := validateLocaleSwitchIndexPath(intent.Images[0].Path, intent.Layout); err != nil {
		return err
	}
	imagePaths := make([]string, 0, len(intent.Images))
	for position, image := range intent.Images {
		wantStaging := filepath.ToSlash(filepath.Join(
			".aoci", "transactions", localeSwitchOperation+"-"+intent.TransactionID,
			"staging", fmt.Sprintf("%02d.post", position),
		))
		if !localeSHA256RE.MatchString(image.PreimageSHA256) || !localeSHA256RE.MatchString(image.PostimageSHA256) ||
			intent.Staging[position].Path != image.Path || intent.Staging[position].SHA256 != image.PostimageSHA256 ||
			intent.Staging[position].StagingRel != wantStaging || intent.Staging[position].ByteSize < 0 {
			return fmt.Errorf("locale_switch_image_invalid: %s", image.Path)
		}
		for _, previous := range imagePaths {
			if localeSwitchPathsEqual(previous, image.Path) {
				return fmt.Errorf("locale_switch_image_invalid: %s", image.Path)
			}
		}
		imagePaths = append(imagePaths, image.Path)
	}
	previousGuardPath := ""
	guardPaths := make([]string, 0, len(intent.Guards))
	for position, guard := range intent.Guards {
		normalized, normalizeErr := afs.NormalizeRelPath(guard.Path)
		if normalizeErr != nil || normalized != guard.Path || guard.Absent == (guard.SHA256 != "") ||
			!guard.Absent && !localeSHA256RE.MatchString(guard.SHA256) {
			return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
		}
		if position > 0 && guard.Path <= previousGuardPath {
			return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
		}
		for _, participant := range imagePaths {
			if localeSwitchPathsEqual(participant, guard.Path) {
				return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
			}
		}
		for _, previous := range guardPaths {
			if localeSwitchPathsEqual(previous, guard.Path) {
				return fmt.Errorf("locale_switch_guard_invalid: %s", guard.Path)
			}
		}
		guardPaths = append(guardPaths, guard.Path)
		previousGuardPath = guard.Path
	}
	transactionID, err := localeSwitchTransactionID(intent.TargetLocale, intent.Layout, intent.Images, intent.Guards)
	if err != nil || transactionID != intent.TransactionID {
		return fmt.Errorf("locale_switch_transaction_identity_invalid")
	}
	want, err := localeSwitchIntentIdentity(intent)
	if err != nil || want != intent.IntentSHA256 {
		return fmt.Errorf("locale_switch_intent_identity_invalid")
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
		return nil, fmt.Errorf("locale_switch_intent_invalid: trailing JSON")
	}
	if err := validateLocaleSwitchIntent(&intent); err != nil {
		return nil, err
	}
	canonical, err := marshalLocaleSwitchIntent(&intent)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, fmt.Errorf("locale_switch_intent_invalid: noncanonical bytes")
	}
	return &intent, nil
}

func localeSwitchIntentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", localeSwitchOperation+"-"+transactionID+".json")
}

func localeSwitchArchivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", localeSwitchOperation+"-"+transactionID+".json")
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
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

func validateLocaleSwitchIndexPath(raw, layout string) (string, error) {
	normalized, err := afs.NormalizeRelPath(raw)
	folded := strings.ToLower(filepath.ToSlash(normalized))
	if err != nil || normalized != raw || folded == ".aoci" || strings.HasPrefix(folded, ".aoci/") ||
		layout == cognition.LayoutVolumesV1 && normalized != "aoci.txt" {
		return "", fmt.Errorf("locale_switch_index_path_invalid")
	}
	return normalized, nil
}

func localeSwitchPathsEqual(left, right string) bool {
	return strings.EqualFold(filepath.ToSlash(left), filepath.ToSlash(right))
}
