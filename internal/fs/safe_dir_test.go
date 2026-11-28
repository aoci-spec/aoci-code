package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckAndEnsureDirInRootRefusesSymlinksAndNonDirs(t *testing.T) {
	t.Run("creates_nested_real_dir", func(t *testing.T) {
		root := t.TempDir()
		if err := CheckDirInRoot(root, ".aoci/backups"); err != nil {
			t.Fatalf("absent nested dir should pass CheckDirInRoot: %v", err)
		}
		dir, err := EnsureDirInRoot(root, ".aoci/backups", 0o755)
		if err != nil {
			t.Fatalf("EnsureDirInRoot failed: %v", err)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected real directory at %s: info=%v err=%v", dir, info, err)
		}
	})

	t.Run("refuses_regular_file_at_target", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".aoci", "backups"), []byte("not a dir\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, fn := range []func() error{
			func() error { return CheckDirInRoot(root, ".aoci/backups") },
			func() error { _, err := EnsureDirInRoot(root, ".aoci/backups", 0o755); return err },
		} {
			err := fn()
			if !errors.Is(err, ErrUnsafeDirPath) || UnsafeDirPathSegment(err) != ".aoci/backups" {
				t.Fatalf("got err=%v segment=%q, want ErrUnsafeDirPath .aoci/backups", err, UnsafeDirPathSegment(err))
			}
		}
	})

	if runtime.GOOS == "windows" {
		return
	}

	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, root, outside string)
		wantPath string
	}{
		{
			name: "backups_symlink_outside_repo",
			setup: func(t *testing.T, root, outside string) {
				if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, ".aoci", "backups")); err != nil {
					t.Fatal(err)
				}
			},
			wantPath: ".aoci/backups",
		},
		{
			name: "backups_symlink_inside_repo",
			setup: func(t *testing.T, root, _ string) {
				inside := filepath.Join(root, "elsewhere")
				if err := os.MkdirAll(inside, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(inside, filepath.Join(root, ".aoci", "backups")); err != nil {
					t.Fatal(err)
				}
			},
			wantPath: ".aoci/backups",
		},
		{
			name: "aoci_parent_symlink_outside_repo",
			setup: func(t *testing.T, root, outside string) {
				if err := os.Symlink(outside, filepath.Join(root, ".aoci")); err != nil {
					t.Fatal(err)
				}
			},
			wantPath: ".aoci",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			tc.setup(t, root, outside)
			for _, fn := range []func() error{
				func() error { return CheckDirInRoot(root, ".aoci/backups") },
				func() error { _, err := EnsureDirInRoot(root, ".aoci/backups", 0o755); return err },
			} {
				err := fn()
				if !errors.Is(err, ErrUnsafeDirPath) || UnsafeDirPathSegment(err) != tc.wantPath {
					t.Fatalf("got err=%v segment=%q, want ErrUnsafeDirPath %q", err, UnsafeDirPathSegment(err), tc.wantPath)
				}
			}
		})
	}
}
