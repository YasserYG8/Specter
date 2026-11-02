//go:build !linux

package main

import "os"

// writevStdout falls back to plain writes where writev(2) is not available; the E2E tests only run on Linux.
func writevStdout(parts ...[]byte) error {
	for _, p := range parts {
		if _, err := os.Stdout.Write(p); err != nil {
			return err
		}
	}
	return nil
}
