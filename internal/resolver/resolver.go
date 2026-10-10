package resolver

import (
	"context"

	"github.com/specter-cli/specter/internal/source"
)

// Resolver discovers target process information from a port or PID.
type Resolver interface {
	// ResolvePort resolves a listening TCP port to an inspected Target.
	ResolvePort(ctx context.Context, port int) (*source.Target, error)

	// ResolvePID inspects a given process ID and returns an inspected Target.
	ResolvePID(ctx context.Context, pid int) (*source.Target, error)
}
