// The consumer of the stable CLI Long Help text assets.
//
// The Cobra commands are still created by each business file; each command takes its stable Long
// Help from this file at construction. This file also carries the Short of the observation commands moved into the assets in this round; the other
// Shorts, flag usages, dynamic reports, error branches, and permission control stay in the original Go files.
//
// This wiring makes textassets the single source of truth for Long Help, leaving no duplicate natural language in the business Go files
// that no longer takes effect but could still drift.
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

const (
	cliHelpAssetFailurePrefix   = "[text asset"
	cliHelpAssetErrorAnnotation = "aoci.cli/help-asset-error"
)

// rootLongHelp returns the stable explanation of the root command.
func rootLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpRootLong,
	)
}

// doctorLongHelp returns the stable explanation of Doctor.
func doctorLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpDoctorLong,
	)
}

// indexLongHelp returns the stable explanation of the Index process group.
func indexLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexLong,
	)
}

// verifyLongHelp returns the stable explanation of Verify's four states and governance debt.
func verifyLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpVerifyLong,
	)
}

// verifyShortHelp returns the stable Verify command summary.
func verifyShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpVerifyShort,
	)
}

// checkLongHelp returns the stable explanation of Check's commit blocking.
func checkLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpCheckLong,
	)
}

// checkShortHelp returns the stable Check command summary.
func checkShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpCheckShort,
	)
}

// scanLongHelp returns the stable explanation of Scan's whitewash prevention.
func scanLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpScanLong,
	)
}

// removeEntryLongHelp returns the stable explanation of the manual Entry removal.
func removeEntryLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpRemoveEntryLong,
	)
}

// indexScoreLongHelp returns the stable explanation of the nine-dimension score.
func indexScoreLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexScoreLong,
	)
}

// indexScoreShortHelp returns the stable Index Score command summary.
func indexScoreShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexScoreShort,
	)
}

// indexInventoryLongHelp returns the Inventory observation description.
func indexInventoryLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexInventoryLong,
	)
}

// indexInventoryShortHelp returns the stable Inventory command summary.
func indexInventoryShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexInventoryShort,
	)
}

// readObservationAuditHelp is the single CLI and documentation source for the
// distinction between formal-asset immutability and local audit writes.
func readObservationAuditHelp() string {
	return cliHelpText(
		textassets.ContractHelpReadObservationAudit,
	)
}

// observationLongHelp appends the shared audit boundary to one command's
// command-specific behavior description.
func observationLongHelp(id textassets.ID) string {
	return cliHelpText(id) + "\n" +
		readObservationAuditHelp()
}

// indexAgentLongHelp returns the stable explanation of the Host Agent protocol capabilities.
func indexAgentLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexAgentLong,
	)
}

// updateEntryLongHelp returns the stable explanation of the single Entry write.
func updateEntryLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpUpdateEntryLong,
	)
}

// cliHelpText renders a stable Help scalar without dynamic variables.
func cliHelpText(
	id textassets.ID,
) string {
	return cliHelpTemplate(id, nil)
}

func cliMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", key)
	}
	return value
}

// cliHelpTemplate serves the Help metadata Cobra can only take as a string. A damaged asset returns a visible diagnosis holding
// the ID and the underlying error; no panic, no fallback to a hard-coded body, and no blocking of unrelated commands.
func cliHelpTemplate(id textassets.ID, data any) string {
	value, err := textassets.RenderScalar(
		textassets.ActiveLocale(),
		id,
		data,
	)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", id)
	}

	return value
}

// installCLIHelpAssetGuard recovers an asset error Cobra can only take as a string into a real command error.
//
// The gate checks only the command fields and flag explanations this Help rendering really uses, not sibling commands, so one
// unrelated damaged Help asset does not block other commands. The check happens before Cobra writes any Help body.
func installCLIHelpAssetGuard(root *cobra.Command) {
	if root == nil {
		return
	}

	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(command *cobra.Command, args []string) {
		if err := commandHelpAssetError(command); err != nil {
			if root.Annotations == nil {
				root.Annotations = map[string]string{}
			}
			root.Annotations[cliHelpAssetErrorAnnotation] =
				err.Error()

			return
		}

		if root.Annotations != nil {
			delete(root.Annotations, cliHelpAssetErrorAnnotation)
		}
		defaultHelp(command, args)
	})
}

// cliHelpExecutionError fetches the error this Help rendering intercepted. The state binds to a single command tree and
// is cleared on every read, so it is never cached across invocations.
func cliHelpExecutionError(root *cobra.Command) error {
	if root == nil || root.Annotations == nil {
		return nil
	}

	message := root.Annotations[cliHelpAssetErrorAnnotation]
	delete(root.Annotations, cliHelpAssetErrorAnnotation)
	if message == "" {
		return nil
	}

	return &ExitError{
		Code: ExitInternal,
		Err:  errors.New(message),
	}
}

// commandHelpAssetError returns the first asset error the current command's Help would consume.
func commandHelpAssetError(command *cobra.Command) error {
	if command == nil {
		return nil
	}

	for _, text := range []string{
		command.Long,
		command.Short,
		command.Example,
	} {
		if isCLIHelpAssetFailure(text) {
			return errors.New(text)
		}
	}

	for _, usages := range []string{
		command.NonInheritedFlags().FlagUsages(),
		command.InheritedFlags().FlagUsages(),
	} {
		position := strings.Index(usages, cliHelpAssetFailurePrefix)
		if position < 0 {
			continue
		}
		message := usages[position:]
		if end := strings.IndexByte(message, '\n'); end >= 0 {
			message = message[:end]
		}
		return errors.New(strings.TrimSpace(message))
	}

	return nil
}

func isCLIHelpAssetFailure(text string) bool {
	return strings.HasPrefix(
		strings.TrimSpace(text),
		cliHelpAssetFailurePrefix,
	)
}

// findCLICommand locates a subcommand from the given root level by level by Name.
//
// It serves byte-level Help tests and other read-only command-tree checks, and does not change the commands.
func findCLICommand(
	root *cobra.Command,
	path ...string,
) *cobra.Command {
	current := root

	for _, name := range path {
		if current == nil {
			return nil
		}

		var found *cobra.Command

		for _, candidate := range current.Commands() {
			if candidate.Name() == name {
				found = candidate
				break
			}
		}

		if found == nil {
			return nil
		}

		current = found
	}

	return current
}
