//go:build !linux

package ebpf

import (
	"context"

	"github.com/YasserYG8/Specter/internal/source"
)

// Start reports that eBPF needs Linux. Specter compiles on macOS and Windows so that its code stays portable, but
// it cannot capture there.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk)
	errs := make(chan error, 1)
	errs <- ErrNeedsLinux
	close(chunks)
	close(errs)
	return chunks, errs
}
