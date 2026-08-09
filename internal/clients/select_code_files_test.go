package clients

import (
	"reflect"
	"testing"
)

func TestSelectCodeFilesPrefersManifestsInDefinitionOrder(t *testing.T) {
	entries := []treeEntry{
		{Path: "README.md", Size: 50},
		{Path: "main.go", Size: 10},
		{Path: "package.json", Size: 30},
		{Path: "go.mod", Size: 100},
	}
	got := selectCodeFiles(entries)
	want := []string{"go.mod", "package.json", "main.go", "README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectCodeFiles() = %v, want %v", got, want)
	}
}

func TestSelectCodeFilesPrefersEntryPointsNext(t *testing.T) {
	entries := []treeEntry{
		{Path: "util.go", Size: 10},
		{Path: "lib/main.rs", Size: 1000},
		{Path: "src/main.java", Size: 900},
		{Path: "index.js", Size: 500},
	}
	got := selectCodeFiles(entries)
	want := []string{"index.js", "lib/main.rs", "src/main.java", "util.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectCodeFiles() = %v, want %v", got, want)
	}
}

func TestSelectCodeFilesFillsRemainingWithSmallestFiles(t *testing.T) {
	entries := []treeEntry{
		{Path: "a.go", Size: 500},
		{Path: "b.go", Size: 100},
		{Path: "c.go", Size: 300},
	}
	got := selectCodeFiles(entries)
	want := []string{"b.go", "c.go", "a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectCodeFiles() = %v, want %v", got, want)
	}
}

func TestSelectCodeFilesExcludesGeneratedAndDependencyPaths(t *testing.T) {
	entries := []treeEntry{
		{Path: "node_modules/foo.js", Size: 100},
		{Path: "vendor/lib.go", Size: 100},
		{Path: "dist/app.js", Size: 100},
		{Path: "build/out", Size: 100},
		{Path: ".git/HEAD", Size: 100},
		{Path: ".venv/bin/x", Size: 100},
		{Path: ".idea/workspace.xml", Size: 100},
		{Path: "package-lock.json", Size: 100},
		{Path: "yarn.lock", Size: 100},
		{Path: "pnpm-lock.yaml", Size: 100},
		{Path: "go.sum", Size: 100},
		{Path: "Cargo.lock", Size: 100},
		{Path: "Gemfile.lock", Size: 100},
		{Path: "composer.lock", Size: 100},
		{Path: "app.min.js", Size: 100},
		{Path: "app.min.css", Size: 100},
		{Path: "app.js.map", Size: 100},
	}
	if got := selectCodeFiles(entries); len(got) != 0 {
		t.Fatalf("selectCodeFiles() = %v, want empty", got)
	}
}

func TestSelectCodeFilesStopsAtMaxCodeFiles(t *testing.T) {
	entries := make([]treeEntry, 0, 8)
	for i := 0; i < 8; i++ {
		entries = append(entries, treeEntry{Path: string(rune('a'+i)) + ".go", Size: 10})
	}
	got := selectCodeFiles(entries)
	if len(got) != maxCodeFiles {
		t.Fatalf("selectCodeFiles() returned %d files, want %d", len(got), maxCodeFiles)
	}
}

func TestSelectCodeFilesStopsAtCharacterBudget(t *testing.T) {
	entries := []treeEntry{
		{Path: "a.go", Size: 2000},
		{Path: "b.go", Size: 2000},
		{Path: "c.go", Size: 2000},
		{Path: "d.go", Size: 2000},
		{Path: "e.go", Size: 2000},
		{Path: "f.go", Size: 2000},
	}
	got := selectCodeFiles(entries)
	if len(got) != 4 {
		t.Fatalf("selectCodeFiles() returned %d files (%v), want 4 (budget exhausted)", len(got), got)
	}
	if contains(got, "e.go") || contains(got, "f.go") {
		t.Fatalf("files beyond the character budget must be dropped: %v", got)
	}
}

func TestSelectCodeFilesSkipsOversizedBlobs(t *testing.T) {
	entries := []treeEntry{
		{Path: "big.go", Size: maxCodeFileBytes + 1},
		{Path: "small.go", Size: 10},
	}
	got := selectCodeFiles(entries)
	if !reflect.DeepEqual(got, []string{"small.go"}) {
		t.Fatalf("selectCodeFiles() = %v, want only small.go", got)
	}
}

func TestSelectCodeFilesIsDeterministic(t *testing.T) {
	entries := []treeEntry{
		{Path: "z.go", Size: 200},
		{Path: "a.go", Size: 100},
		{Path: "b.go", Size: 100},
		{Path: "go.mod", Size: 20},
	}
	first := selectCodeFiles(entries)
	for i := 0; i < 5; i++ {
		if got := selectCodeFiles(entries); !reflect.DeepEqual(got, first) {
			t.Fatalf("selectCodeFiles() not deterministic: %v != %v", got, first)
		}
	}
}

func TestSelectCodeFilesReturnsEmptyForNoCandidates(t *testing.T) {
	if got := selectCodeFiles(nil); len(got) != 0 {
		t.Fatalf("selectCodeFiles(nil) = %v, want empty", got)
	}
	if got := selectCodeFiles([]treeEntry{{Path: "tree.md", Size: 10, Type: "tree"}}); len(got) != 0 {
		t.Fatalf("selectCodeFiles(tree) = %v, want empty", got)
	}
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
