package main

import (
	"strings"
	"testing"
)

func TestSlowdownNoticeFor(t *testing.T) {
	if got := slowdownNoticeFor("strace", ""); got != slowdownNotice {
		t.Errorf("strace should print the notice, got %q", got)
	}
	for _, name := range []string{"docker", "file", "journal", ""} {
		if got := slowdownNoticeFor(name, ""); got != "" {
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

func TestSlowdownNoticeFor_EndsWithTheReasonForNotUsingEBPF(t *testing.T) {
	got := slowdownNoticeFor("strace", "Run with sudo to use the fast eBPF engine.")
	if !strings.HasPrefix(got, slowdownNotice+" ") || !strings.HasSuffix(got, "Run with sudo to use the fast eBPF engine.") {
		t.Errorf("notice = %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Error("the notice must stay one line")
	}
	// A reason only belongs to the strace notice: the other sources do not slow the target, so there is no notice
	if got := slowdownNoticeFor("ebpf", "anything"); got != "" {
		t.Errorf("the ebpf source prints no slowdown notice, got %q", got)
	}
	if got := slowdownNoticeFor("docker", "anything"); got != "" {
		t.Errorf("docker prints no slowdown notice, got %q", got)
	}
	// strace chosen by the user: no reason and no tip
	if got := slowdownNoticeFor("strace", ""); got != slowdownNotice {
		t.Errorf("notice = %q", got)
	}
}
