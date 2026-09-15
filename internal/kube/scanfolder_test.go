package kube

import (
	"os"
	"path/filepath"
	"testing"
)

// A Kubernetes Secret mounted as a volume is a folder of symlinks into a hidden,
// versioned directory. The links must be read; the hidden directory must not be
// offered on its own.
func TestScanFolderFollowsSecretVolumeSymlinks(t *testing.T) {
	dir := t.TempDir()
	versioned := filepath.Join(dir, "..2026_09_15_10_00_00.000000001")
	if err := os.Mkdir(versioned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versioned, "prod.yaml"), []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(versioned), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "prod.yaml"), filepath.Join(dir, "prod.yaml")); err != nil {
		t.Fatal(err)
	}
	// A link to a directory is not a kubeconfig.
	if err := os.Symlink(versioned, filepath.Join(dir, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	got := scanFolder(dir)
	want := filepath.Join(resolve(dir), "prod.yaml")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("scanFolder = %v, want [%s]", got, want)
	}
}
