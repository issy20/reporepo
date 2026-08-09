package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/issy20/reporepo/internal/presentation"
)

func plainPresenter(out io.Writer) *presentation.Renderer {
	return presentation.NewRenderer(out, presentation.Capabilities{Width: 80})
}

func decoratedPresenter(out io.Writer) *presentation.Renderer {
	return presentation.NewRenderer(out, presentation.Capabilities{Decorated: true, Width: 80})
}

func TestRootPresentationContract(t *testing.T) {
	root := newRootCommand(commandDependencies{presenter: plainPresenter})
	if !root.SilenceUsage || !root.SilenceErrors {
		t.Fatalf("silence flags = %v, %v", root.SilenceUsage, root.SilenceErrors)
	}
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GitHub リポジトリ", "Usage", "run", "config", "version", "where", "analyze", "reporepo config"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("plain help has ANSI: %q", out.String())
	}
}

func TestDecoratedVersionAndWhereUseCurrentCommandWriter(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"where"}} {
		out := &bytes.Buffer{}
		root := newRootCommand(commandDependencies{presenter: decoratedPresenter, configPath: func() (string, error) { return "/full/config/path", nil }, dataPath: func() (string, error) { return "/full/data/path", nil }})
		root.SetOut(out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("%v output not decorated: %q", args, out.String())
		}
	}
}

func TestExecuteRootRendersCommandErrorOnceWithoutUsage(t *testing.T) {
	errOut := &bytes.Buffer{}
	root := newRootCommand(commandDependencies{presenter: plainPresenter, run: func() error { return errors.New("起動できませんでした") }})
	root.SetErr(errOut)
	root.SetArgs([]string{"run"})
	err := executeRoot(root, plainPresenter)
	if err == nil {
		t.Fatal("error = nil")
	}
	if strings.Count(errOut.String(), "起動できませんでした") != 1 {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "Usage:") || !strings.Contains(errOut.String(), "ERROR:") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestNewRootCommandPublishesCommands(t *testing.T) {
	root := NewRootCommand()

	want := map[string]bool{"run": false, "config": false, "version": false, "where": false, "analyze": false}
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

func TestWhereDoesNotExposePathResolutionErrors(t *testing.T) {
	secret := "private-home-path"
	for _, tc := range []struct {
		name string
		deps commandDependencies
		want string
	}{
		{"config", commandDependencies{configPath: func() (string, error) { return "", errors.New(secret) }, dataPath: func() (string, error) { return "data", nil }}, "設定ファイルの保存先を解決できませんでした"},
		{"data", commandDependencies{configPath: func() (string, error) { return "config", nil }, dataPath: func() (string, error) { return "", errors.New(secret) }}, "データファイルの保存先を解決できませんでした"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRootCommand(tc.deps)
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(errOut)
			root.SetArgs([]string{"where"})
			err := root.Execute()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(out.String()+errOut.String()+err.Error(), secret) {
				t.Fatalf("path error leaked: %q %q %v", out, errOut, err)
			}
		})
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
