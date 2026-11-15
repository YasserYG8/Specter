//go:build !linux

package ui

import (
	"context"
	"errors"
	"os"
)

// CanReadKeys is always false here: Specter attaches only on Linux, so there is no stream to read keys during. The
// file exists so Specter still compiles for macOS and Windows.
func CanReadKeys(stdin, stderr *os.File) bool { return false }

// KeyReader has no terminal code outside Linux.
type KeyReader struct{}

// NewKeyReader makes a reader that cannot start.
func NewKeyReader(f *os.File) *KeyReader { return &KeyReader{} }

// Start returns an error: reading keys is not supported here.
func (k *KeyReader) Start(ctx context.Context) (<-chan Key, error) {
	return nil, errors.New("reading keys is not supported on this system")
}

// Stop does nothing.
func (k *KeyReader) Stop() {}
