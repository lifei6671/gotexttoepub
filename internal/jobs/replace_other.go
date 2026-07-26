//go:build !windows

package jobs

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
