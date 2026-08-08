package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var archiveDocuments = []string{
	"CHANGELOG.md",
	"LICENSE",
	"NOTICE",
	"PATENTS",
	"README.md",
	"README.zh-CN.md",
	"THIRD-PARTY-NOTICES",
	"TRADEMARKS",
}

func main() {
	flags := flag.NewFlagSet("archive-smoke", flag.ExitOnError)
	dist := flags.String("dist", "dist", "GoReleaser distribution directory")
	flags.Parse(os.Args[1:])
	if err := smoke(*dist); err != nil {
		writeArchiveStatus(os.Stderr, "error", "archive_smoke_failed", err.Error())
		os.Exit(1)
	}
	writeArchiveStatus(os.Stdout, "ok", "archives_verified", *dist)
}

func smoke(dist string) error {
	tarArchives, err := filepath.Glob(filepath.Join(dist, "*.tar.gz"))
	if err != nil {
		return err
	}
	zipArchives, err := filepath.Glob(filepath.Join(dist, "*.zip"))
	if err != nil {
		return err
	}
	if len(tarArchives)+len(zipArchives) == 0 {
		return fmt.Errorf("no release archives found in %s", dist)
	}
	for _, path := range tarArchives {
		names, err := tarNames(path)
		if err != nil {
			return err
		}
		if err := requireNames(path, names, archiveNames("aoci")); err != nil {
			return err
		}
	}
	for _, path := range zipArchives {
		names, err := zipNames(path)
		if err != nil {
			return err
		}
		if err := requireNames(path, names, archiveNames("aoci.exe")); err != nil {
			return err
		}
	}
	var linuxAMD64 []string
	for _, path := range tarArchives {
		if strings.HasSuffix(path, "_linux_amd64.tar.gz") {
			linuxAMD64 = append(linuxAMD64, path)
		}
	}
	if len(linuxAMD64) != 1 {
		return fmt.Errorf("expected exactly one linux amd64 archive, got %d", len(linuxAMD64))
	}
	return executeLinuxArchive(linuxAMD64[0])
}

func archiveNames(binary string) []string {
	names := append([]string(nil), archiveDocuments...)
	return append(names, binary)
}

func tarNames(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive %s contains non-regular entry %s", path, header.Name)
		}
		names = append(names, header.Name)
	}
	return names, nil
}

func zipNames(path string) ([]string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("archive %s contains non-regular entry %s", path, file.Name)
		}
		names = append(names, file.Name)
	}
	return names, nil
}

func requireNames(path string, actual, expected []string) error {
	actual = append([]string(nil), actual...)
	expected = append([]string(nil), expected...)
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("unexpected archive contents in %s: %v", path, actual)
	}
	return nil
}

func executeLinuxArchive(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	tempDir, err := os.MkdirTemp("", "aoci-archive-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	binaryPath := filepath.Join(tempDir, "aoci")
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "aoci" {
			continue
		}
		output, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, reader)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("linux archive has no aoci binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binaryPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("execute packaged binary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if !strings.HasPrefix(strings.TrimSpace(string(output)), "aoci version ") {
		return fmt.Errorf("unexpected packaged version output %q", strings.TrimSpace(string(output)))
	}
	return nil
}

func writeArchiveStatus(file *os.File, status, code, detail string) {
	_ = json.NewEncoder(file).Encode(map[string]string{"status": status, "code": code, "detail": detail})
}
