package main

import (
	"bytes"
	"testing"
)

func TestCLI_InvalidArgument(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"abc"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-numeric argument, got nil")
	}
}

func TestCLI_NegativePort(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-5"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative port/pid argument, got nil")
	}
}

func TestBuildVersionString(t *testing.T) {
	str := buildVersionString()
	if str == "" {
		t.Fatal("expected non-empty version string")
	}
	if !bytes.Contains([]byte(str), []byte(version)) {
		t.Errorf("expected version %q in string, got %s", version, str)
	}
	if !bytes.Contains([]byte(str), []byte("commit:")) {
		t.Errorf("expected commit marker in version string, got %s", str)
	}
	if !bytes.Contains([]byte(str), []byte("built at:")) {
		t.Errorf("expected built at marker in version string, got %s", str)
	}
}

func TestCLI_VersionSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error executing version command: %v", err)
	}
}

func TestCLI_ZeroArgsAccepted(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil && bytes.Contains([]byte(err.Error()), []byte("accepts 1 arg(s)")) {
		t.Fatalf("unexpected arg count error: %v", err)
	}
}
