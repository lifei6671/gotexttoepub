package cmd

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/lifei6671/gotexttoepub/internal/buildinfo"
)

func TestLogServiceStartingIncludesBuildInfo(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo}))

	logServiceStarting(logger)

	for _, want := range []string{
		"level=INFO",
		"msg=服务正在启动",
		"version=" + buildinfo.Version(),
		"build_time=" + buildinfo.BuildTime(),
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("startup log does not contain %q: %s", want, output.String())
		}
	}
}
