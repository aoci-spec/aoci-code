package cli

import "testing"

// The Help guard must recognize every failure marker the CLI renders, or a
// damaged asset is printed with exit 0 instead of failing the command.
func TestHelpAssetGuardRecognizesEveryFailureMarker(t *testing.T) {
	for name, text := range map[string]string{
		"message":  cliMessage("no.such.message.key"),
		"template": cliHelpText("no.such.help.id"),
	} {
		if !isCLIHelpAssetFailure(text) {
			t.Fatalf("%s failure %q is not recognized by the Help guard", name, text)
		}
	}
}
