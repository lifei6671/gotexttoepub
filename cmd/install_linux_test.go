//go:build linux

package cmd

import (
	"context"
	"os"
	"testing"
)

func TestSystemdInstallerCanUpgradeManagedInstallation(t *testing.T) {
	installer, sourcePath, binaryPath, unitPath := newTestSystemdInstaller(t)
	installer.runCommand = func(context.Context, string, ...string) error { return nil }

	if err := installer.install(context.Background()); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("updated executable"), 0o755); err != nil {
		t.Fatalf("update source binary: %v", err)
	}
	if err := installer.install(context.Background()); err != nil {
		t.Fatalf("upgrade managed installation: %v", err)
	}

	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read upgraded binary: %v", err)
	}
	if string(content) != "updated executable" {
		t.Fatalf("upgraded binary content = %q", content)
	}
	if err := ensureManagedUnit(unitPath); err != nil {
		t.Fatalf("upgraded unit should remain managed: %v", err)
	}
}

func TestCopyFileAtomicSupportsSourceEqualToTarget(t *testing.T) {
	path := t.TempDir() + "/gotexttoepub"
	if err := os.WriteFile(path, []byte("running executable"), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	if err := copyFileAtomic(path, path, 0o755); err != nil {
		t.Fatalf("atomic self replacement: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced executable: %v", err)
	}
	if string(content) != "running executable" {
		t.Fatalf("replaced executable content = %q", content)
	}
}
