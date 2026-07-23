package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBinaryNonInteractiveCommandsAndConfigFlow(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "reporepo")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(tmp, "go-cache"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	home := filepath.Join(tmp, "home")
	configHome := filepath.Join(tmp, "config")
	dataHome := filepath.Join(tmp, "data")
	configPath := filepath.Join(configHome, "reporepo", "config.json")
	if runtime.GOOS == "darwin" {
		configPath = filepath.Join(home, "Library", "Application Support", "reporepo", "config.json")
	}
	env := append(filteredEnvironment(), "HOME="+home, "XDG_CONFIG_HOME="+configHome, "XDG_DATA_HOME="+dataHome, "APPDATA="+configHome)
	run := func(input string, args ...string) (string, string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	for _, tc := range []struct {
		name     string
		args     []string
		contains string
	}{
		{"help", []string{"--help"}, "Available Commands"},
		{"version", []string{"version"}, "reporepo 0.1.0"},
		{"where", []string{"where"}, configPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := run("", tc.args...)
			if err != nil {
				t.Fatalf("%v: stderr=%q", err, stderr)
			}
			if !strings.Contains(stdout, tc.contains) {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("where created config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "reporepo", "data.json")); !os.IsNotExist(err) {
		t.Fatalf("where created data: %v", err)
	}

	secret := "integration-anthropic-secret"
	stdout, stderr, err := run("\n"+secret+"\n\nclaude\nja\ny\n", "config")
	if err != nil {
		t.Fatalf("config: %v stderr=%q stdout=%q", err, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatal("config output leaked secret")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}

	if err := os.Remove(configPath); err != nil {
		t.Fatalf("remove test config: %v", err)
	}
	stdout, stderr, err = run("", "run")
	if err == nil {
		t.Fatal("run without AI keys succeeded")
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "ANTHROPIC_API_KEY または OPENAI_API_KEY") {
		t.Fatalf("output = %q", combined)
	}
	if strings.Contains(combined, tmp) {
		t.Fatalf("output leaked internal path: %q", combined)
	}
}

func filteredEnvironment() []string {
	blocked := map[string]bool{"HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "APPDATA": true, "GITHUB_TOKEN": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true}
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if !blocked[name] {
			env = append(env, item)
		}
	}
	return env
}
