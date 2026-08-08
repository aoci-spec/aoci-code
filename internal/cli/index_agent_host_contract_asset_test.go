// Host-Agent运行时说明和Help资产迁移的字节级兼容测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readHostContractGolden(
	t *testing.T,
	name string,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			name,
		),
	)
	if err != nil {
		t.Fatalf(
			"read Host-Agent contract golden %s: %v",
			name,
			err,
		)
	}

	return string(data)
}

func renderHostRuntimeContract(
	t *testing.T,
	goos string,
) string {
	t.Helper()
	instructions, err := agentRuntimeInstructionsFor(goos)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(
		instructions,
		"\n",
	) + "\n"
}

func renderHostStageHelpContract(
	t *testing.T,
	rootPath []string,
) string {
	t.Helper()

	root := newRootCmd()
	children := root.Commands()
	t.Cleanup(func() {
		root.RemoveCommand(
			children...,
		)
	})

	command, _, err := root.Find(
		rootPath,
	)
	if err != nil ||
		command == nil {
		t.Fatalf(
			"find Host-Agent Stage command %v: %v",
			rootPath,
			err,
		)
	}

	requestFileFlag := command.Flags().Lookup(
		"request-file",
	)
	stdinFlag := command.Flags().Lookup(
		"stdin-json",
	)

	if requestFileFlag == nil ||
		stdinFlag == nil {
		t.Fatalf(
			"Host-Agent Stage command %v is missing request flags",
			rootPath,
		)
	}

	return "LONG:\n" +
		command.Long +
		"\nREQUEST_FILE:\n" +
		requestFileFlag.Usage +
		"\nSTDIN_JSON:\n" +
		stdinFlag.Usage +
		"\n"
}

func renderHostGuideHelpContract(
	t *testing.T,
) string {
	t.Helper()

	root := newRootCmd()
	children := root.Commands()
	t.Cleanup(func() {
		root.RemoveCommand(
			children...,
		)
	})

	command, _, err := root.Find(
		[]string{
			"index",
			"agent",
			"guide",
		},
	)
	if err != nil ||
		command == nil {
		t.Fatalf(
			"find Host-Agent Guide command: %v",
			err,
		)
	}

	return "LONG:\n" +
		command.Long +
		"\n"
}

func TestHostRuntimeLinuxMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostRuntimeContract(
		t,
		"linux",
	)
	expected := readHostContractGolden(
		t,
		"host_runtime_linux.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Linux Host-Agent runtime contract changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestHostRuntimeWindowsMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostRuntimeContract(
		t,
		"windows",
	)
	expected := readHostContractGolden(
		t,
		"host_runtime_windows.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Windows Host-Agent runtime contract changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestHostEntriesStageHelpMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostStageHelpContract(
		t,
		[]string{
			"index",
			"agent",
			"stage",
		},
	)
	expected := readHostContractGolden(
		t,
		"host_help_entries_stage.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Entries Stage Help changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestHostHeaderStageHelpMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostStageHelpContract(
		t,
		[]string{
			"index",
			"agent",
			"header",
			"stage",
		},
	)
	expected := readHostContractGolden(
		t,
		"host_help_header_stage.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Header Stage Help changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestHostCurationStageHelpMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostStageHelpContract(
		t,
		[]string{
			"index",
			"agent",
			"curation",
			"stage",
		},
	)
	expected := readHostContractGolden(
		t,
		"host_help_curation_stage.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Curation Stage Help changed during asset migration:\n%s",
			actual,
		)
	}
}

func TestHostGuideHelpMatchesGoldenByteForByte(
	t *testing.T,
) {
	actual := renderHostGuideHelpContract(
		t,
	)
	expected := readHostContractGolden(
		t,
		"host_help_guide.txt",
	)

	if actual != expected {
		t.Fatalf(
			"Guide Help changed during asset migration:\n%s",
			actual,
		)
	}
}
