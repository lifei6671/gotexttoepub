package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestEpubCommandWithoutRequiredFileReturnsErrorInsteadOfPanic(t *testing.T) {
	var buffer bytes.Buffer
	app := &cli.App{
		Commands: []*cli.Command{newEpubCommand(), newRulesCommand()},
		Writer:   &buffer,
	}

	var runErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("expected missing file to return error, got panic: %v", recovered)
			}
		}()
		runErr = app.Run([]string{"gotexttoepub", "epub"})
	}()

	if runErr == nil {
		t.Fatal("expected missing file to return an error")
	}
	if !strings.Contains(runErr.Error(), "Required flag") && !strings.Contains(runErr.Error(), "file") {
		t.Fatalf("expected error to mention required file flag, got: %v", runErr)
	}
}
