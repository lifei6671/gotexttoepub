//go:build !windows

package cmd

import "os"

func currentEffectiveUID() int {
	return os.Geteuid()
}
