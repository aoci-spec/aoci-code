package textassets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"
	"unicode"
)

var (
	runtimeValidationOnce sync.Once
	runtimeValidationErr  error
)

// ValidateRuntime validates the immutable embedded catalog once per process.
// CLI commands and MCP startup call it before side effects so catalog damage,
// cross-Locale key drift, and format-signature drift fail closed.
func ValidateRuntime() error {
	runtimeValidationOnce.Do(func() {
		runtimeValidationErr = Validate()
	})
	return runtimeValidationErr
}

var (
	templateVariableName = regexp.MustCompile(
		`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`,
	)
	localeName        = regexp.MustCompile(`^[a-z]{2}-[A-Z]{2}$`)
	consumerReference = regexp.MustCompile(
		`^[A-Za-z0-9_./-]+\.go:[A-Za-z_][A-Za-z0-9_]*(?:,[A-Za-z0-9_./-]+\.go:[A-Za-z_][A-Za-z0-9_]*)*$`,
	)
)

func decodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func decodeManifestFragment(data []byte) (manifestFragment, error) {
	var fragment manifestFragment
	if err := decodeStrictJSON(data, &fragment); err != nil {
		return manifestFragment{}, err
	}

	return fragment, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("manifest contains trailing JSON value")
		}
		return fmt.Errorf("manifest contains trailing data: %w", err)
	}

	return nil
}

// validateManifestMetadata严格校验清单本身，但不读取任何资源正文。
func validateManifestMetadata(manifest Manifest) error {
	if manifest.Version <= 0 {
		return fmt.Errorf("text asset manifest version must be positive")
	}
	official, development, err := validateLocaleLifecycle(manifest)
	if err != nil {
		return err
	}

	assetIDs := map[string]struct{}{}
	declaredPaths := map[string]string{}
	for _, asset := range manifest.Assets {
		assetID := strings.TrimSpace(asset.ID)
		if assetID == "" || assetID != asset.ID {
			return fmt.Errorf(
				"text asset manifest contains invalid asset id %q",
				asset.ID,
			)
		}
		if _, exists := assetIDs[assetID]; exists {
			return fmt.Errorf(
				"text asset manifest contains duplicate asset id %q",
				assetID,
			)
		}
		assetIDs[assetID] = struct{}{}

		if err := validateAssetMetadata(asset, manifest); err != nil {
			return fmt.Errorf("asset %q: %w", assetID, err)
		}
		cleanPath, err := validateAssetPath(asset.Path)
		if err != nil {
			return fmt.Errorf("asset %q: %w", assetID, err)
		}
		if previousID, exists := declaredPaths[cleanPath]; exists {
			return fmt.Errorf(
				"assets %q and %q declare duplicate path %q",
				previousID,
				assetID,
				cleanPath,
			)
		}
		declaredPaths[cleanPath] = assetID
	}

	if len(official)+len(development) == 0 {
		return fmt.Errorf("text asset manifest declares no locales")
	}

	return nil
}

// validateManifestFS是完整发布校验：正式Locale必须完整，开发中Locale允许
// 部分资源，但所有实际存在的资源仍必须通过变量与合同校验。
func validateManifestFS(assetFS fs.FS, manifest Manifest) error {
	if err := validateManifestMetadata(manifest); err != nil {
		return err
	}
	official, development, _ := validateLocaleLifecycle(manifest)

	declaredPaths := make(map[string]string, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		cleanPath, _ := validateAssetPath(asset.Path)
		declaredPaths[cleanPath] = asset.ID

		for locale := range official {
			if _, err := validateLocalizedAsset(assetFS, locale, cleanPath, asset, true); err != nil {
				return err
			}
		}
		for locale := range development {
			if _, err := validateLocalizedAsset(assetFS, locale, cleanPath, asset, false); err != nil {
				return err
			}
		}
	}

	for locale := range official {
		if err := rejectUnregisteredFiles(assetFS, locale, declaredPaths, true); err != nil {
			return err
		}
	}
	for locale := range development {
		if err := rejectUnregisteredFiles(assetFS, locale, declaredPaths, false); err != nil {
			return err
		}
	}
	if err := rejectUndeclaredLocaleDirectories(assetFS, official, development); err != nil {
		return err
	}
	if err := validateOfficialJSONParity(assetFS, manifest, official); err != nil {
		return err
	}

	return nil
}

func validateOfficialJSONParity(
	assetFS fs.FS,
	manifest Manifest,
	official map[string]struct{},
) error {
	locales := make([]string, 0, len(official))
	for locale := range official {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	for _, asset := range manifest.Assets {
		if !strings.HasSuffix(asset.Path, ".json") {
			continue
		}
		var referenceShape string
		var referenceMessages map[string]string
		for position, locale := range locales {
			data, err := fs.ReadFile(assetFS, path.Join(locale, asset.Path))
			if err != nil {
				return err
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				return fmt.Errorf("asset %q locale %q contains invalid JSON: %w", asset.ID, locale, err)
			}
			shape := jsonValueShape(value)
			if position == 0 {
				referenceShape = shape
			} else if shape != referenceShape {
				return fmt.Errorf("asset %q JSON structure differs between official locales", asset.ID)
			}
			if asset.ID == string(ContractUIMessages) {
				messages, decodeErr := decodeMessageBundle(data)
				if decodeErr != nil {
					return fmt.Errorf("asset %q locale %q: %w", asset.ID, locale, decodeErr)
				}
				if position == 0 {
					referenceMessages = messages
					continue
				}
				for key, format := range messages {
					if string(formatSignature(format)) != string(formatSignature(referenceMessages[key])) {
						return fmt.Errorf(
							"asset %q message %q format schema differs between official locales",
							asset.ID,
							key,
						)
					}
				}
			}
		}
	}
	return nil
}

func jsonValueShape(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var builder strings.Builder
		builder.WriteByte('{')
		for _, key := range keys {
			builder.WriteString(key)
			builder.WriteByte(':')
			builder.WriteString(jsonValueShape(typed[key]))
			builder.WriteByte(';')
		}
		builder.WriteByte('}')
		return builder.String()
	case []any:
		var builder strings.Builder
		builder.WriteByte('[')
		for _, item := range typed {
			builder.WriteString(jsonValueShape(item))
			builder.WriteByte(';')
		}
		builder.WriteByte(']')
		return builder.String()
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func validateLocaleLifecycle(
	manifest Manifest,
) (map[string]struct{}, map[string]struct{}, error) {
	official := map[string]struct{}{}
	development := map[string]struct{}{}

	for _, locale := range manifest.OfficialLocales {
		if err := addLocale(official, locale, "official"); err != nil {
			return nil, nil, err
		}
	}
	for _, locale := range manifest.DevelopmentLocales {
		if err := addLocale(development, locale, "development"); err != nil {
			return nil, nil, err
		}
		if _, exists := official[locale]; exists {
			return nil, nil, fmt.Errorf(
				"text asset locale %q is both official and development",
				locale,
			)
		}
	}

	if strings.TrimSpace(manifest.DefaultLocale) == "" {
		return nil, nil, fmt.Errorf("text asset default locale is empty")
	}
	if _, exists := official[manifest.DefaultLocale]; !exists {
		return nil, nil, fmt.Errorf(
			"default locale %q is not official",
			manifest.DefaultLocale,
		)
	}

	return official, development, nil
}

func addLocale(target map[string]struct{}, locale, state string) error {
	if locale != strings.TrimSpace(locale) || !localeName.MatchString(locale) {
		return fmt.Errorf(
			"text asset manifest contains invalid %s locale %q",
			state,
			locale,
		)
	}
	if _, exists := target[locale]; exists {
		return fmt.Errorf(
			"text asset manifest contains duplicate %s locale %q",
			state,
			locale,
		)
	}
	target[locale] = struct{}{}

	return nil
}

func validateAssetMetadata(asset ManifestAsset, manifest Manifest) error {
	kindNamespaces := map[string]string{
		"contract": "contracts/",
		"prompt":   "prompts/",
		"template": "templates/",
	}
	namespace, kindExists := kindNamespaces[asset.Kind]
	if !kindExists {
		return fmt.Errorf("unsupported kind %q", asset.Kind)
	}
	if !strings.HasPrefix(asset.ID, namespace) {
		return fmt.Errorf(
			"kind %q requires asset id namespace %q",
			asset.Kind,
			namespace,
		)
	}
	if strings.TrimSpace(asset.Description) == "" {
		return fmt.Errorf("description is empty")
	}
	if !consumerReference.MatchString(asset.UsedBy) {
		return fmt.Errorf(
			"used_by must be a comma-separated path.go:Symbol list: %q",
			asset.UsedBy,
		)
	}
	if asset.Kind == "prompt" && asset.EnforcedBy == nil {
		return fmt.Errorf("prompt enforced_by boundary is not declared")
	}
	if err := validateUniqueStrings(asset.ProtocolTokens, "protocol token"); err != nil {
		return err
	}
	if err := validateUniqueStrings(asset.EnforcedBy, "enforcement reference"); err != nil {
		return err
	}
	for _, reference := range asset.EnforcedBy {
		if !consumerReference.MatchString(reference) || strings.Contains(reference, ",") {
			return fmt.Errorf(
				"enforced_by must contain path.go:Symbol references: %q",
				reference,
			)
		}
	}

	declaredLocales := append(cloneStrings(manifest.OfficialLocales), manifest.DevelopmentLocales...)
	for locale, anchors := range asset.LocaleAnchors {
		if !containsString(declaredLocales, locale) {
			return fmt.Errorf("locale anchors declare unknown locale %q", locale)
		}
		if err := validateUniqueStrings(anchors, "locale anchor"); err != nil {
			return fmt.Errorf("locale %q: %w", locale, err)
		}
	}

	seenVariables := map[string]struct{}{}
	for _, variable := range asset.Variables {
		if variable != strings.TrimSpace(variable) ||
			(variable != "." && !templateVariableName.MatchString(variable)) {
			return fmt.Errorf("invalid template variable %q", variable)
		}
		if _, exists := seenVariables[variable]; exists {
			return fmt.Errorf("duplicate template variable %q", variable)
		}
		seenVariables[variable] = struct{}{}
	}
	if !sort.StringsAreSorted(asset.Variables) {
		return fmt.Errorf("template variables must be sorted")
	}

	return nil
}

func validateUniqueStrings(values []string, kind string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("contains invalid %s %q", kind, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("contains duplicate %s %q", kind, value)
		}
		seen[value] = struct{}{}
	}

	return nil
}

func validateLocalizedAsset(
	assetFS fs.FS,
	locale,
	assetPath string,
	asset ManifestAsset,
	required bool,
) (bool, error) {
	embeddedPath := locale + "/" + assetPath
	data, err := fs.ReadFile(assetFS, embeddedPath)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(
			"asset %q is missing for locale %q at %q: %w",
			asset.ID,
			locale,
			embeddedPath,
			err,
		)
	}
	if err := validateLocalizedAssetText(locale, asset, string(data)); err != nil {
		return true, err
	}

	return true, nil
}

func validateLocalizedAssetText(locale string, asset ManifestAsset, text string) error {
	if locale == "en-US" && strings.IndexFunc(text, func(r rune) bool {
		return unicode.Is(unicode.Han, r)
	}) >= 0 {
		return fmt.Errorf(
			"asset %q locale %q contains unexpected Han text",
			asset.ID,
			locale,
		)
	}
	for _, token := range asset.ProtocolTokens {
		if !strings.Contains(text, token) {
			return fmt.Errorf(
				"asset %q locale %q is missing protocol token %q",
				asset.ID,
				locale,
				token,
			)
		}
	}
	for _, anchor := range asset.LocaleAnchors[locale] {
		if !strings.Contains(text, anchor) {
			return fmt.Errorf(
				"asset %q locale %q is missing locale anchor %q",
				asset.ID,
				locale,
				anchor,
			)
		}
	}

	actualVariables, err := extractTemplateVariables(asset.ID, text)
	if err != nil {
		return fmt.Errorf(
			"asset %q locale %q has invalid template syntax: %w",
			asset.ID,
			locale,
			err,
		)
	}
	expectedVariables := cloneStrings(asset.Variables)
	if !sameStrings(actualVariables, expectedVariables) {
		return fmt.Errorf(
			"asset %q locale %q template variable set mismatch: actual=%v declared=%v",
			asset.ID,
			locale,
			actualVariables,
			expectedVariables,
		)
	}

	return nil
}

func rejectUnregisteredFiles(
	assetFS fs.FS,
	locale string,
	declaredPaths map[string]string,
	required bool,
) error {
	if _, err := fs.Stat(assetFS, locale); err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read locale directory %q: %w", locale, err)
	}

	return fs.WalkDir(assetFS, locale, func(
		filePath string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relativePath := strings.TrimPrefix(filePath, locale+"/")
		if _, exists := declaredPaths[relativePath]; !exists {
			return fmt.Errorf(
				"locale %q contains unregistered text asset %q",
				locale,
				relativePath,
			)
		}

		return nil
	})
}

func rejectUndeclaredLocaleDirectories(
	assetFS fs.FS,
	official,
	development map[string]struct{},
) error {
	entries, err := fs.ReadDir(assetFS, ".")
	if err != nil {
		return fmt.Errorf("read embedded text asset root: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == manifestFragmentsDir {
			continue
		}
		if _, exists := official[entry.Name()]; exists {
			continue
		}
		if _, exists := development[entry.Name()]; exists {
			continue
		}
		return fmt.Errorf(
			"embedded text asset directory %q is not a declared locale",
			entry.Name(),
		)
	}

	return nil
}

func extractTemplateVariables(name, source string) ([]string, error) {
	compiled, err := template.New(name).Parse(source)
	if err != nil {
		return nil, err
	}

	variables := map[string]struct{}{}
	walkTemplateNode(compiled.Tree.Root, variables)

	result := make([]string, 0, len(variables))
	for variable := range variables {
		result = append(result, variable)
	}
	sort.Strings(result)

	return result, nil
}

func walkTemplateNode(node parse.Node, variables map[string]struct{}) {
	if node == nil {
		return
	}
	value := reflect.ValueOf(node)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return
	}

	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			walkTemplateNode(child, variables)
		}
	case *parse.ActionNode:
		walkTemplateNode(current.Pipe, variables)
	case *parse.IfNode:
		walkTemplateBranch(current.Pipe, current.List, current.ElseList, variables)
	case *parse.RangeNode:
		walkTemplateBranch(current.Pipe, current.List, current.ElseList, variables)
	case *parse.WithNode:
		walkTemplateBranch(current.Pipe, current.List, current.ElseList, variables)
	case *parse.TemplateNode:
		walkTemplateNode(current.Pipe, variables)
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			walkTemplateNode(command, variables)
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			walkTemplateNode(argument, variables)
		}
	case *parse.FieldNode:
		if len(current.Ident) > 0 {
			variables[strings.Join(current.Ident, ".")] = struct{}{}
		}
	case *parse.DotNode:
		variables["."] = struct{}{}
	case *parse.ChainNode:
		walkTemplateNode(current.Node, variables)
		if len(current.Field) > 0 {
			variables[strings.Join(current.Field, ".")] = struct{}{}
		}
	}
}

func walkTemplateBranch(
	pipe *parse.PipeNode,
	list,
	elseList *parse.ListNode,
	variables map[string]struct{},
) {
	walkTemplateNode(pipe, variables)
	walkTemplateNode(list, variables)
	walkTemplateNode(elseList, variables)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}
