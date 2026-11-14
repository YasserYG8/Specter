package main

import (
	"context"
	"errors"
	"os"

	"github.com/fatih/color"

	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/ui"
)

// pickTarget runs the interactive picker, for a run with no target on the command line, and resolves the service
// the user chose. The picker selects one target. It returns nil, nil when the user closed it without a choice.
func pickTarget(ctx context.Context, res *resolver.DefaultResolver) (*source.Target, error) {
	services, err := res.DiscoverServices(ctx)
	if err != nil {
		return nil, errors.New("failed to discover active services: " + err.Error())
	}

	selected, err := ui.SelectService(services, ui.PickerConfig{
		In:       os.Stdin,
		Out:      os.Stderr,
		UseColor: !flagNoColor && !color.NoColor,
	})
	if err != nil {
		if errors.Is(err, ui.ErrPickerAborted) {
			return nil, nil
		}
		return nil, err
	}
	if selected == nil {
		return nil, nil
	}

	var target *source.Target
	if selected.Kind == resolver.ServiceKindDocker {
		if selected.Port > 0 {
			if t, err := res.ResolvePort(ctx, selected.Port); err == nil {
				target = t
			}
		}
		if target == nil {
			targetName := selected.ContainerName
			if targetName == "" {
				targetName = selected.ContainerID
			}
			t, err := res.ResolveContainer(ctx, targetName)
			if err != nil {
				return nil, err
			}
			target = t
		}
	} else {
		if selected.Port > 0 {
			t, err := res.ResolvePort(ctx, selected.Port)
			if err != nil {
				return nil, err
			}
			target = t
		} else if selected.PID > 0 {
			t, err := res.ResolvePID(ctx, selected.PID)
			if err != nil {
				return nil, err
			}
			target = t
		}
	}

	if target == nil {
		return nil, errors.New("failed to resolve target")
	}
	return target, nil
}
