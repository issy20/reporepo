package cmd

import (
	"bytes"
	"errors"
	"testing"
)

func TestNewRootCommandPublishesCommands(t *testing.T) {
	root := NewRootCommand()

	want := map[string]bool{"run": false, "config": false, "version": false, "where": false}
	for _, command := range root.Commands() {
		if _, ok := want[command.Name()]; ok {
			want[command.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q was not published", name)
		}
	}
}

func TestRootAndRunStartTUI(t *testing.T) {
	for _, args := range [][]string{nil, {"run"}} {
		t.Run(commandName(args), func(t *testing.T) {
			calls := 0
			root := newRootCommand(commandDependencies{
				run: func() error {
					calls++
					return nil
				},
			})
			root.SetArgs(args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if calls != 1 {
				t.Fatalf("run calls = %d, want 1", calls)
			}
		})
	}
}

func TestRunReturnsStartupError(t *testing.T) {
	want := errors.New("startup failed")
	root := newRootCommand(commandDependencies{run: func() error { return want }})
	root.SetArgs([]string{"run"})

	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("Execute() error = %v, want %v", err, want)
	}
}

func commandName(args []string) string {
	if len(args) == 0 {
		return "root"
	}
	return args[0]
}
