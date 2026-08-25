// Package authoringcontract assembles the deterministic, Locale-bound model
// instructions for Volumes authoring from formal Meta dictionaries and current
// machine contracts. It never changes, completes, or substitutes Meta bytes.
package authoringcontract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// ParsedMeta is the immutable formal Meta input used by the assembler.
type ParsedMeta struct {
	Raw            []byte
	Dictionaries   map[string]*index.TagDict
	SQuotaContract string
}

// MachineFacts contains the current non-semantic write contract. Callers pass
// it explicitly so tests and every runtime surface exercise the same assembly.
type MachineFacts struct {
	TagForm                 string
	CodeIdentityExample     string
	DatabaseIdentityExample string
	RelationSeparator       string
	NoRelation              string
	FRASLimits              machinecontract.FRASV2Limits
}

// Input contains every authority needed to assemble affected-domain guidance.
type Input struct {
	Meta    ParsedMeta
	Domains []string
	Machine MachineFacts
	Locale  string
}

// Output maps only onto existing Guide/Maintain fields.
type Output struct {
	AuthoringMeta string
	Instructions  []string
	Examples      map[string]string
}

// CurrentMachineFacts returns the single compiled FRAS, Tag, and relation
// contract used by both Guide and Maintain.
func CurrentMachineFacts() MachineFacts {
	return MachineFacts{
		TagForm:                 "compact A+B+C+[D]+E",
		CodeIdentityExample:     "code:path/to/file.go",
		DatabaseIdentityExample: "database://source/namespace/table",
		RelationSeparator:       ",",
		NoRelation:              "-",
		FRASLimits:              machinecontract.ObjectFRASV2Limits(),
	}
}

// ParseMeta binds the exact formal bytes to complete affected-domain
// dictionaries. Missing or conflicting dictionaries fail closed.
func ParseMeta(raw []byte, domains []string) (ParsedMeta, error) {
	parsed := ParsedMeta{
		Raw: append([]byte{}, raw...), Dictionaries: map[string]*index.TagDict{},
		SQuotaContract: index.EffectiveSQuotaContract(string(raw)),
	}
	for _, domain := range orderedDomains(domains) {
		dictionary := index.ExtractScopedTagDict(string(raw), domain)
		if dictionary == nil || !dictionary.HasObjectContract() {
			problems := []string{"dictionary=missing"}
			if dictionary != nil {
				problems = dictionary.ObjectContractProblems()
			}
			return ParsedMeta{}, fmt.Errorf("meta_tag_dictionary_invalid: domain=%s; %s", domain, strings.Join(problems, ","))
		}
		parsed.Dictionaries[domain] = dictionary
	}
	return parsed, nil
}

// Build is the shared runtime entry point used by Guide and Maintain.
func Build(metaRaw []byte, domains []string, locale string) (Output, error) {
	parsed, err := ParseMeta(metaRaw, domains)
	if err != nil {
		return Output{}, err
	}
	return Assemble(Input{Meta: parsed, Domains: domains, Machine: CurrentMachineFacts(), Locale: locale})
}

// Assemble renders deterministic Code then Database instructions. The output
// carries the formal Meta bytes verbatim and validates every concrete example
// through the formal object parser, FRAS validator, and current dictionary.
func Assemble(input Input) (Output, error) {
	if !textassets.IsOfficialLocale(input.Locale) {
		return Output{}, fmt.Errorf("authoring_contract_locale_invalid: %s", input.Locale)
	}
	output := Output{AuthoringMeta: string(input.Meta.Raw), Instructions: []string{}, Examples: map[string]string{}}
	metaInstruction, err := textassets.Message(input.Locale, "volumes.authoring.meta")
	if err != nil {
		return Output{}, err
	}
	output.Instructions = append(output.Instructions, metaInstruction)
	batchInstruction, err := textassets.Message(input.Locale, "volumes.authoring.batch_identity")
	if err != nil {
		return Output{}, err
	}
	output.Instructions = append(output.Instructions, batchInstruction)
	transportInstruction, err := textassets.Message(input.Locale, "volumes.authoring.transport")
	if err != nil {
		return Output{}, err
	}
	output.Instructions = append(output.Instructions, transportInstruction)
	localeInstruction, err := textassets.Message(input.Locale, "volumes.authoring.locale", input.Locale)
	if err != nil {
		return Output{}, err
	}
	output.Instructions = append(output.Instructions, localeInstruction)
	for _, domain := range orderedDomains(input.Domains) {
		dictionary := input.Meta.Dictionaries[domain]
		if dictionary == nil || !dictionary.HasObjectContract() {
			return Output{}, fmt.Errorf("meta_tag_dictionary_invalid: domain=%s", domain)
		}
		tag, tagErr := calibrationTag(domain, dictionary)
		if tagErr != nil {
			return Output{}, fmt.Errorf("meta_tag_dictionary_invalid: domain=%s; %w", domain, tagErr)
		}
		contractKey := "volumes.authoring." + domain + ".contract"
		contract, messageErr := textassets.Message(input.Locale, contractKey,
			input.Machine.TagForm, dictionary.Contract(), input.Machine.FRASLimits.FMaxRunes,
			input.Machine.FRASLimits.RMaxRunes, input.Machine.FRASLimits.RMaxItems,
			input.Machine.FRASLimits.AMaxRunes, input.Machine.FRASLimits.AMaxItems,
			input.Meta.SQuotaContract, input.Machine.CodeIdentityExample,
			input.Machine.DatabaseIdentityExample, input.Machine.RelationSeparator,
			input.Machine.NoRelation,
		)
		if messageErr != nil {
			return Output{}, messageErr
		}
		output.Instructions = append(output.Instructions, contract)
		if domain == cognition.ScopeDatabase {
			rules, renderErr := textassets.RenderScalar(input.Locale, textassets.PromptDatabaseEntryAuthoring, nil)
			if renderErr != nil {
				return Output{}, renderErr
			}
			output.Instructions = append(output.Instructions, rules)
		}
		example, messageErr := textassets.Message(input.Locale, "volumes.authoring."+domain+".example", tag)
		if messageErr != nil {
			return Output{}, messageErr
		}
		if findings := cognition.ValidateVolumeAuthoringExample(domain, example, dictionary); len(findings) > 0 {
			return Output{}, fmt.Errorf("authoring_contract_example_invalid: domain=%s; rule_code=%s", domain, findings[0].RuleCode)
		}
		output.Examples[domain] = example
		output.Instructions = append(output.Instructions, example)
	}
	return output, nil
}

func orderedDomains(domains []string) []string {
	present := map[string]bool{}
	for _, domain := range domains {
		if domain == cognition.ScopeCode || domain == cognition.ScopeDatabase {
			present[domain] = true
		}
	}
	ordered := make([]string, 0, len(present))
	for _, domain := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		if present[domain] {
			ordered = append(ordered, domain)
		}
	}
	return ordered
}

func calibrationTag(domain string, dictionary *index.TagDict) (string, error) {
	axis := func(values map[string]bool, preferred string) (string, error) {
		if values[preferred] {
			return preferred, nil
		}
		keys := make([]string, 0, len(values))
		for value := range values {
			keys = append(keys, value)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return "", fmt.Errorf("dictionary axis is empty")
		}
		return keys[0], nil
	}
	preferredA, preferredB := starterCalibrationSymbols(domain, dictionary)
	a, err := axis(dictionary.A, preferredA)
	if err != nil {
		return "", err
	}
	b, err := axis(dictionary.B, preferredB)
	if err != nil {
		return "", err
	}
	c, err := axis(dictionary.C, "7")
	if err != nil {
		return "", err
	}
	e, err := axis(dictionary.E, "T")
	if err != nil {
		return "", err
	}
	return a + b + c + e, nil
}

func starterCalibrationSymbols(domain string, dictionary *index.TagDict) (string, string) {
	definitionIs := func(axis, symbol string, expected ...string) bool {
		actual, ok := dictionary.Definition(axis, symbol)
		if !ok {
			return false
		}
		for _, value := range expected {
			if actual == value {
				return true
			}
		}
		return false
	}
	switch domain {
	case cognition.ScopeCode:
		if definitionIs("A", "E", "-EntryBoundary", "-入口边界") &&
			definitionIs("B", "G", "-CrossDomain", "-跨域通用") {
			return "E", "G"
		}
	case cognition.ScopeDatabase:
		if definitionIs("A", "E", "-EntityMaster", "-实体主表") &&
			definitionIs("B", "I", "-IdentityAccess", "-身份权限") {
			return "E", "I"
		}
	}
	return "", ""
}
