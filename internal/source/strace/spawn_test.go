package strace

import (
	"testing"
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
