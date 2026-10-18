package strace

import (
	"reflect"
	"testing"

	"github.com/specter-cli/specter/internal/source"
)

func TestNewSource_Options(t *testing.T) {
	sDefault := NewSource()
	if sDefault.options.FollowTree {
		t.Errorf("expected default FollowTree to be false, got true")
	}

	sTree := NewSource(Options{FollowTree: true})
	if !sTree.options.FollowTree {
		t.Errorf("expected FollowTree to be true, got false")
	}

	if sTree.Name() != "strace" {
		t.Errorf("expected Name() to be 'strace', got %q", sTree.Name())
	}
}

func TestBuildStraceArgs_MultiPID(t *testing.T) {
	t.Run("single target without children", func(t *testing.T) {
		target := source.Target{
			PID: 596,
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "8192", "-e", "trace=write", "-p", "596"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("target with child workers", func(t *testing.T) {
		target := source.Target{
			PID:      596,
			Children: []int{629, 630},
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "8192", "-e", "trace=write", "-p", "596", "-p", "629", "-p", "630"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("deduplicates self PID and redundant children", func(t *testing.T) {
		target := source.Target{
			PID:      596,
			Children: []int{596, 629, 629, 0, -1},
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "8192", "-e", "trace=write", "-p", "596", "-p", "629"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("caps children at max 16 to avoid excessive arguments", func(t *testing.T) {
		children := make([]int, 30)
		for i := 0; i < 30; i++ {
			children[i] = 1000 + i
		}
		target := source.Target{
			PID:      500,
			Children: children,
		}
		args := BuildStraceArgs(target)

		// 8 base args + 1 parent (-p 500) + 16 children * 2 (-p <pid>) = 8 + 32 = 40
		// Wait: base args are "-f", "-qq", "-xx", "-s", "8192", "-e", "trace=write" (7 items) + "-p", "500" (2 items) = 9 items
		// Each child adds 2 items: "-p", "<pid>"
		// 9 + (16 * 2) = 41
		if len(args) != 41 {
			t.Errorf("expected 41 args (capped at 16 children), got %d: %v", len(args), args)
		}
	})
}
