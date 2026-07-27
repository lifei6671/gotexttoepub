package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestSystemdInstallerInstall(t *testing.T) {
	installer, sourcePath, binaryPath, unitPath := newTestSystemdInstaller(t)
	var commands []string
	installer.runCommand = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return nil
	}

	if err := installer.install(context.Background()); err != nil {
		t.Fatalf("install systemd service: %v", err)
	}

	sourceContent, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source binary: %v", err)
	}
	installedContent, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(installedContent, sourceContent) {
		t.Fatalf("installed binary content = %q, want %q", installedContent, sourceContent)
	}

	unitContent, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read installed unit: %v", err)
	}
	unit := string(unitContent)
	for _, expected := range []string{
		managedUnitMarker,
		"ExecStart=" + binaryPath + " serve",
		"DynamicUser=yes",
		"StateDirectory=gotexttoepub",
		"EnvironmentFile=-/etc/default/gotexttoepub",
		"Restart=on-failure",
	} {
		if !strings.Contains(unit, expected) {
			t.Fatalf("unit does not contain %q:\n%s", expected, unit)
		}
	}

	expectedCommands := []string{
		"/usr/bin/systemctl --version",
		"/usr/bin/systemctl daemon-reload",
		"/usr/bin/systemctl enable gotexttoepub.service",
		"/usr/bin/systemctl restart gotexttoepub.service",
		"/usr/bin/systemctl is-active --quiet gotexttoepub.service",
	}
	if !reflect.DeepEqual(commands, expectedCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, expectedCommands)
	}
}

func TestSystemdInstallerRejectsUnsupportedEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*systemdInstaller)
		wantError string
	}{
		{
			name: "non linux",
			configure: func(installer *systemdInstaller) {
				installer.goos = "windows"
			},
			wantError: "仅支持 Linux",
		},
		{
			name: "non root",
			configure: func(installer *systemdInstaller) {
				installer.effectiveUID = func() int { return 1000 }
			},
			wantError: "root 权限",
		},
		{
			name: "systemd not running",
			configure: func(installer *systemdInstaller) {
				installer.systemdRuntimeDir = filepath.Join(t.TempDir(), "missing")
			},
			wantError: "未检测到正在运行的 systemd",
		},
		{
			name: "systemctl missing",
			configure: func(installer *systemdInstaller) {
				installer.lookPath = func(string) (string, error) {
					return "", errors.New("not found")
				}
			},
			wantError: "找不到 systemctl",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installer, _, binaryPath, unitPath := newTestSystemdInstaller(t)
			test.configure(&installer)
			installer.runCommand = func(context.Context, string, ...string) error {
				t.Fatal("unsupported environment must not invoke systemctl")
				return nil
			}

			err := installer.install(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("install error = %v, want containing %q", err, test.wantError)
			}
			if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
				t.Fatalf("binary should not be installed, stat error = %v", err)
			}
			if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
				t.Fatalf("unit should not be installed, stat error = %v", err)
			}
		})
	}
}

func TestSystemdInstallerRejectsUnmanagedUnit(t *testing.T) {
	installer, _, binaryPath, unitPath := newTestSystemdInstaller(t)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(unitPath, []byte("[Unit]\nDescription=custom\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged unit: %v", err)
	}
	installer.runCommand = func(_ context.Context, _ string, args ...string) error {
		if reflect.DeepEqual(args, []string{"--version"}) {
			return nil
		}
		t.Fatalf("unmanaged unit must stop before systemctl mutation: %v", args)
		return nil
	}

	err := installer.install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("install error = %v, want unmanaged unit rejection", err)
	}
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatalf("binary should not be installed, stat error = %v", err)
	}
	content, readErr := os.ReadFile(unitPath)
	if readErr != nil {
		t.Fatalf("read unmanaged unit: %v", readErr)
	}
	if string(content) != "[Unit]\nDescription=custom\n" {
		t.Fatalf("unmanaged unit was modified: %q", content)
	}
}

func TestSystemdInstallerStopsAfterSystemctlFailure(t *testing.T) {
	installer, _, _, _ := newTestSystemdInstaller(t)
	sentinel := errors.New("enable failed")
	var commands []string
	installer.runCommand = func(_ context.Context, _ string, args ...string) error {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		if command == "enable gotexttoepub.service" {
			return sentinel
		}
		return nil
	}

	err := installer.install(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("install error = %v, want wrapping sentinel", err)
	}
	expected := []string{"--version", "daemon-reload", "enable gotexttoepub.service"}
	if !reflect.DeepEqual(commands, expected) {
		t.Fatalf("commands = %#v, want %#v", commands, expected)
	}
}

func TestInstallCommandPrintsSuccess(t *testing.T) {
	installer, _, _, _ := newTestSystemdInstaller(t)
	installer.runCommand = func(context.Context, string, ...string) error { return nil }
	var output bytes.Buffer
	app := &cli.App{
		Commands: []*cli.Command{newInstallCommand(installer)},
		Writer:   &output,
	}

	if err := app.Run([]string{"gotexttoepub", "install"}); err != nil {
		t.Fatalf("run install command: %v", err)
	}
	for _, expected := range []string{"已安装并启动", "systemctl status", "journalctl"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, output.String())
		}
	}
}

func TestRenderSystemdUnitRejectsInvalidPath(t *testing.T) {
	tests := []string{"relative/gotexttoepub", "/usr/local/bin/gotexttoepub\nExecStart=/bin/sh"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, err := renderSystemdUnit(path); err == nil {
				t.Fatalf("renderSystemdUnit(%q) should fail", path)
			}
		})
	}
}

func newTestSystemdInstaller(t *testing.T) (systemdInstaller, string, string, string) {
	t.Helper()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "download", "gotexttoepub")
	binaryPath := filepath.Join(root, "usr", "local", "bin", "gotexttoepub")
	unitPath := filepath.Join(root, "etc", "systemd", "system", systemdServiceName)
	systemdRuntimeDir := filepath.Join(root, "run", "systemd", "system")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.MkdirAll(systemdRuntimeDir, 0o755); err != nil {
		t.Fatalf("create fake systemd runtime directory: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("test executable"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	installer := systemdInstaller{
		goos:              "linux",
		effectiveUID:      func() int { return 0 },
		executable:        func() (string, error) { return sourcePath, nil },
		lookPath:          func(string) (string, error) { return "/usr/bin/systemctl", nil },
		runCommand:        func(context.Context, string, ...string) error { return nil },
		binaryPath:        binaryPath,
		unitPath:          unitPath,
		systemdRuntimeDir: systemdRuntimeDir,
	}
	return installer, sourcePath, binaryPath, unitPath
}
