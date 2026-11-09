package main

import (
	"context"

	"github.com/YasserYG8/Specter/internal/source"
)

// startNow starts a source and reports at once if it could not start. The eBPF source loads its program inside
// Start, so when the kernel refuses it, the refusal is already waiting on the error channel when Start returns. A
// source that is running returns its channels and a nil error.
func startNow(ctx context.Context, src source.Source, target source.Target) (<-chan source.Chunk, <-chan error, error) {
	chunks, errs := src.Start(ctx, target)
	select {
	case err, ok := <-errs:
		if ok && err != nil {
			return nil, nil, err
		}
	default:
	}
	return chunks, errs, nil
}
