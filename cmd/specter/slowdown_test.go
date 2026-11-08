package main

import (
	"strings"
	"testing"
)

func TestSlowdownNoticeFor(t *testing.T) {
	if got := slowdownNoticeFor("strace"); got != slowdownNotice {
		t.Errorf("strace should print the notice, got %q", got)
	}
	for _, name := range []string{"docker", "file", "journal", ""} {
		if got := slowdownNoticeFor(name); got != "" {
			t.Errorf("%q does not slow the target, but got the notice %q", name, got)
		}
	}
}

func TestSlowdownNoticeText(t *testing.T) {
	for _, want := range []string{"slows the target", "development and test servers", "--duration"} {
		if !strings.Contains(slowdownNotice, want) {
			t.Errorf("the notice should contain %q, got %q", want, slowdownNotice)
		}
	}
	if strings.Contains(slowdownNotice, "\n") {
		t.Error("the notice must be a single line")
	}
}
