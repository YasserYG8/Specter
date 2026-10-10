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
