package main

import (
	"errors"
	"testing"
)

func TestRunReturnsExitCode(t *testing.T) {
	if got := run(func() error { return nil }); got != 0 {
		t.Fatalf("success exit code = %d, want 0", got)
	}
	if got := run(func() error { return errors.New("failed") }); got != 1 {
		t.Fatalf("failure exit code = %d, want 1", got)
	}
}
