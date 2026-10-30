// The authoring contract a Maintain response carries: once per process and
// refresh generation, then by reference.
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/textassets"
)

// maintainInstructionsRef stands in for instructions and authoring_meta on a
// candidate-carrying response after this process has sent them in the
// current refresh generation. The contract is the same text on every round
// of one logical plan (the exact Meta bytes and the locale's instructions),
// and on a 24 KiB budget it cost a quarter of every response; a Host that no
// longer holds it (a new process, a compaction it did not report) asks for it
// back with include_instructions=true. A context-compaction reload that
// completes an Overview advances the generation, so the Maintain after it
// carries the contract again without any Host flag.
type maintainInstructionsRef struct {
	SHA256                string `json:"sha256"`
	DeliveredInGeneration int    `json:"delivered_in_generation"`
	RecallInput           string `json:"recall_input"`
}

const maintainRecallInput = "include_instructions"

// authoringContractDigest identifies one contract text: the Meta bytes and
// the instruction list, each delimited, under a versioned domain prefix.
func authoringContractDigest(authoringMeta string, instructions []string) string {
	digest := sha256.New()
	digest.Write([]byte("volumes-authoring-contract/v1\x00"))
	digest.Write([]byte(authoringMeta))
	digest.Write([]byte{0})
	for _, instruction := range instructions {
		digest.Write([]byte(instruction))
		digest.Write([]byte{0x1e})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// authoringContractOnWire answers whether the next response carries the
// complete contract with this digest: always when the Host asks for it or
// there is no session, otherwise only until this process has sent it in the
// current refresh generation. The generation it answers is the one a
// reference reports.
func (session *cognitionRefreshSession) authoringContractOnWire(digest string, include bool) (bool, int) {
	if session == nil {
		return true, 0
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if include {
		return true, session.generation
	}
	delivered, ok := session.authoringContracts[digest]
	return !ok || delivered != session.generation, session.generation
}

func (session *cognitionRefreshSession) recordAuthoringContract(digest string) {
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.authoringContracts == nil {
		session.authoringContracts = map[string]int{}
	}
	session.authoringContracts[digest] = session.generation
}

// attachAuthoringContract puts the contract on a candidate-carrying response,
// or the reference to the one this process already sent. extra carries what
// this response adds beyond the contract (the optimization review note); it
// rides in instructions either way, so a reference never hides it.
func attachAuthoringContract(result *volumeMaintainResult, contract authoringcontract.Output, extra []string, session *cognitionRefreshSession, include bool) {
	digest := authoringContractDigest(contract.AuthoringMeta, contract.Instructions)
	onWire, generation := session.authoringContractOnWire(digest, include)
	if onWire {
		result.AuthoringMeta = contract.AuthoringMeta
		result.Instructions = append(append([]string{}, contract.Instructions...), extra...)
		session.recordAuthoringContract(digest)
		return
	}
	result.InstructionsRef = &maintainInstructionsRef{SHA256: digest, DeliveredInGeneration: generation, RecallInput: maintainRecallInput}
	if len(extra) > 0 {
		result.Instructions = append([]string{}, extra...)
	}
}

// codeContractOnWire answers, before a Code batch is cut, whether the
// response will carry the Code contract, so the transport budget reserves it
// only on the rounds that carry it. A response that also issues Database
// candidates carries the Code-and-Database contract, a different text that
// may go on the wire while this answered false; the Database part of a batch
// is bounded by count, not by the byte budget, so that round is at most the
// contract's size over, as every round was before 0.2.0.
func codeContractOnWire(set *cognition.Set, session *cognitionRefreshSession, include bool) bool {
	if set == nil || include {
		return true
	}
	contract, err := authoringcontract.Build(set.Meta.Raw, []string{cognition.ScopeCode}, textassets.ActiveLocale())
	if err != nil {
		return true
	}
	onWire, _ := session.authoringContractOnWire(authoringContractDigest(contract.AuthoringMeta, contract.Instructions), false)
	return onWire
}
