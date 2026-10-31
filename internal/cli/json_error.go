// The unified JSON failure envelope of the ordinary CLI.
//
// Successful business objects keep their existing top-level protocol and are not forced into an ok/data shell.
// This envelope serves only a --json request that fails before any business JSON was written.
package cli

import (
	"encoding/json"
	"errors"
	"io"
	"runtime"
)

const cliJSONErrorVersion = 1

// cliJSONErrorEnvelope is the stable failure protocol of the ordinary CLI.
type cliJSONErrorEnvelope struct {
	Version   int    `json:"version"`
	OK        bool   `json:"ok"`
	ExitCode  int    `json:"exit_code"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
}

// writeCLIJSONError writes the single JSON error object.
func writeCLIJSONError(
	writer io.Writer,
	err error,
	exitCode int,
) error {
	return writeCLIJSONErrorForPlatform(
		writer,
		err,
		exitCode,
		runtime.GOOS,
	)
}

// writeCLIJSONErrorForPlatform writes the error envelope for an explicit platform, for Windows behaviour tests.
func writeCLIJSONErrorForPlatform(
	writer io.Writer,
	err error,
	exitCode int,
	goos string,
) error {
	envelope := cliJSONErrorEnvelope{
		Version:  cliJSONErrorVersion,
		OK:       false,
		ExitCode: exitCode,
		ErrorCode: classifyCLIErrorCode(
			err,
			exitCode,
		),
		Message: localizedCLIErrorMessage(err, exitCode),
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		envelope.Details = exitErr.Details
	}

	data, marshalErr := json.MarshalIndent(
		envelope,
		"",
		"  ",
	)
	if marshalErr != nil {
		return marshalErr
	}

	data = append(
		data,
		'\n',
	)

	return writeCLIDataForPlatform(
		writer,
		data,
		true,
		goos,
	)
}

// classifyCLIErrorCode maps an error to the stable machine classification.
//
// When the caller already knows an exact domain failure such as write_conflict, it passes it through explicitly via
// ExitError.MachineCode; that takes precedence over the generic exit-code mapping but does not change the process exit code. Without a domain code
// the existing drift/invalid/config/internal classification applies.
//
// exit 1 may mean governance drift or come from a not yet migrated command such as Doctor, so only
// an explicit ExitError business drift is classified as drift, and any other exit 1 is command_failed.
func classifyCLIErrorCode(
	err error,
	exitCode int,
) string {
	var exitErr *ExitError

	if errors.As(
		err,
		&exitErr,
	) {
		if exitErr.MachineCode != "" {
			return exitErr.MachineCode
		}

		switch exitErr.Code {
		case ExitDrift:
			return "drift"
		case ExitInvalid:
			return "invalid"
		case ExitConfig:
			return "config"
		case ExitInternal:
			return "internal"
		}
	}

	switch exitCode {
	case ExitInvalid:
		return "invalid"
	case ExitConfig:
		return "config"
	case ExitInternal:
		return "internal"
	default:
		return "command_failed"
	}
}
