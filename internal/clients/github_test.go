package clients

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseRepositoryInput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{name: "short form", input: "owner/repo", wantOwner: "owner", wantRepo: "repo"},
		{name: "URL", input: "https://github.com/owner/repo", wantOwner: "owner", wantRepo: "repo"},
		{name: "URL trailing slash", input: "https://github.com/owner/repo/", wantOwner: "owner", wantRepo: "repo"},
		{name: "URL dot git", input: "https://github.com/owner/repo.git", wantOwner: "owner", wantRepo: "repo"},
		{name: "empty", input: "", wantErr: true},
		{name: "missing repo", input: "owner", wantErr: true},
		{name: "too many segments", input: "owner/repo/extra", wantErr: true},
		{name: "other host", input: "https://example.com/owner/repo", wantErr: true},
		{name: "whitespace padding", input: "  owner/repo  ", wantOwner: "owner", wantRepo: "repo"},
		{name: "contains space", input: "owner /repo", wantErr: true},
		{name: "contains query", input: "owner/repo?query=1", wantErr: true},
		{name: "contains fragment", input: "owner/repo#readme", wantErr: true},
		{name: "contains escaped slash", input: "owner%2Frepo", wantErr: true},
		{name: "dot owner", input: "./repo", wantErr: true},
		{name: "dot dot owner", input: "../repo", wantErr: true},
		{name: "URL dot dot owner", input: "https://github.com/../repo", wantErr: true},
		{name: "percent encoded dot dot", input: "%2e%2e/repo", wantErr: true},
		{name: "percent encoded query", input: "owner/repo%3Fquery", wantErr: true},
		{name: "control character", input: "owner/repo\x00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepositoryInput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Fatalf("got %q/%q, want %q/%q", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestClientFetchRepository(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = true
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/repos/owner/repo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":42,"forks_count":7,"language":"Go","topics":["tui"],"html_url":"https://github.com/owner/repo","license":{"spdx_id":"MIT"},"updated_at":"2026-07-14T00:00:00Z"}`))
		case "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100,"Shell":20}`))
		case "/repos/owner/repo/readme":
			if got := r.Header.Get("Accept"); got != "application/vnd.github.raw" {
				t.Errorf("Accept = %q", got)
			}
			_, _ = w.Write([]byte("# README"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "token")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Meta.FullName != "owner/repo" || got.Meta.Stars != 42 || got.Meta.Forks != 7 || got.Meta.License != "MIT" {
		t.Fatalf("unexpected metadata: %#v", got.Meta)
	}
	if !reflect.DeepEqual(got.Meta.Languages, map[string]int{"Go": 100, "Shell": 20}) {
		t.Fatalf("languages = %#v", got.Meta.Languages)
	}
	if got.README != "# README" {
		t.Fatalf("README = %q", got.README)
	}
	for _, path := range []string{"/repos/owner/repo", "/repos/owner/repo/languages", "/repos/owner/repo/readme"} {
		if !seen[path] {
			t.Errorf("endpoint was not requested: %s", path)
		}
	}
}

func TestClientFetchRepositoryMeta(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = true
		switch r.URL.Path {
		case "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":42,"forks_count":7,"language":"Go","topics":["tui"],"html_url":"https://github.com/owner/repo","license":{"spdx_id":"MIT"},"updated_at":"2026-07-14T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	meta, err := client.FetchRepositoryMeta(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepositoryMeta: %v", err)
	}
	if meta.FullName != "owner/repo" || meta.Stars != 42 || meta.Forks != 7 || meta.License != "MIT" || meta.Description != "desc" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	if !meta.UpdatedAt.Equal(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v", meta.UpdatedAt)
	}
	if len(meta.Languages) != 0 {
		t.Fatalf("Languages = %#v, want empty for meta-only fetch", meta.Languages)
	}
	if len(seen) != 1 || !seen["/repos/owner/repo"] {
		t.Fatalf("requested paths = %v, want only /repos/owner/repo", seen)
	}
}

func TestClientFetchRepositoryMeta_InvalidInput(t *testing.T) {
	client := NewGitHubClient(nil, "https://api.github.com", "")
	for _, tc := range []struct{ owner, repo string }{
		{"owner", "repo/extra"},
		{"  owner", "repo"},
		{"%2e%2e", "repo"},
		{"owner", "repo\x00"},
	} {
		if _, err := client.FetchRepositoryMeta(context.Background(), tc.owner, tc.repo); err == nil {
			t.Errorf("expected error for owner=%q repo=%q", tc.owner, tc.repo)
		}
	}
}

func TestClientFetchRepositoryMeta_ErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    map[string]string
		want       error
		wantStatus int
	}{
		{name: "not found", status: http.StatusNotFound, want: ErrNotFound},
		{name: "rate limited", status: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, want: ErrRateLimited},
		{name: "server error", status: http.StatusInternalServerError, wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			client := NewGitHubClient(server.Client(), server.URL, "")
			_, err := client.FetchRepositoryMeta(context.Background(), "owner", "repo")
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if tt.wantStatus != 0 {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tt.wantStatus {
					t.Fatalf("error = %v, want HTTP status %d", err, tt.wantStatus)
				}
			}
		})
	}
}

func TestClientErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    map[string]string
		want       error
		wantStatus int
	}{
		{name: "not found", status: http.StatusNotFound, want: ErrNotFound},
		{name: "rate limited", status: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, want: ErrRateLimited},
		{name: "server error", status: http.StatusInternalServerError, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			client := NewGitHubClient(server.Client(), server.URL, "")
			_, err := client.FetchRepository(context.Background(), "owner", "repo")
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if tt.wantStatus != 0 {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tt.wantStatus {
					t.Fatalf("error = %v, want HTTP status %d", err, tt.wantStatus)
				}
			}
		})
	}
}

func TestClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewGitHubClient(server.Client(), server.URL, "")
	_, err := client.FetchRepository(ctx, "owner", "repo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestClientFetchRepository_InvalidInput(t *testing.T) {
	client := NewGitHubClient(nil, "https://api.github.com", "")

	invalidInputs := []struct {
		owner string
		repo  string
	}{
		{owner: "owner", repo: "repo/extra"},
		{owner: "owner/extra", repo: "repo"},
		{owner: "owner", repo: "../repo"},
		{owner: "owner", repo: "repo?query=1"},
		{owner: "owner", repo: "repo#fragment"},
		{owner: "  owner", repo: "repo"},
		{owner: "%2e%2e", repo: "repo"},
		{owner: "owner", repo: "repo%3Fquery"},
		{owner: "owner", repo: "repo\x00"},
	}

	for _, tt := range invalidInputs {
		_, err := client.FetchRepository(context.Background(), tt.owner, tt.repo)
		if err == nil {
			t.Errorf("expected error for invalid owner=%q, repo=%q, but got nil", tt.owner, tt.repo)
		}
	}
}

func TestClientFetchRepository_WithCodeContext(t *testing.T) {
	var contentPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":1,"forks_count":1,"language":"Go","default_branch":"main"}`))
		case r.URL.Path == "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case r.URL.Path == "/repos/owner/repo/readme":
			_, _ = w.Write([]byte("# README"))
		case r.URL.Path == "/repos/owner/repo/git/trees/main":
			if r.URL.RawQuery != "recursive=1" {
				t.Errorf("tree query = %q, want recursive=1", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"tree":[{"path":"go.mod","mode":"100644","type":"blob","size":100},{"path":"main.go","mode":"100644","type":"blob","size":10},{"path":"README.md","mode":"100644","type":"blob","size":50}]}`))
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
			path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
			if got := r.Header.Get("Accept"); got != "application/vnd.github.raw" {
				t.Errorf("contents Accept = %q", got)
			}
			contentPaths = append(contentPaths, path)
			_, _ = w.Write([]byte("content of " + path))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Code == nil || len(got.Code.Files) != 3 {
		t.Fatalf("Code = %#v, want 3 files", got.Code)
	}
	wantPaths := []string{"go.mod", "main.go", "README.md"}
	for i, want := range wantPaths {
		if got.Code.Files[i].Path != want || got.Code.Files[i].Content != "content of "+want {
			t.Fatalf("file[%d] = %#v, want path=%q", i, got.Code.Files[i], want)
		}
	}
	if !reflect.DeepEqual(contentPaths, wantPaths) {
		t.Fatalf("contents requested in order %v, want %v", contentPaths, wantPaths)
	}
}

func TestClientFetchRepository_TreeFailureFallsBackToNilCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":1,"forks_count":1,"language":"Go","default_branch":"main"}`))
		case "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case "/repos/owner/repo/readme":
			_, _ = w.Write([]byte("# README"))
		case "/repos/owner/repo/git/trees/main":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Code != nil {
		t.Fatalf("Code = %#v, want nil fallback", got.Code)
	}
}

func TestClientFetchRepository_EmptyDefaultBranchSkipsCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":1,"forks_count":1,"language":"Go"}`))
		case "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case "/repos/owner/repo/readme":
			_, _ = w.Write([]byte("# README"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Code != nil {
		t.Fatalf("Code = %#v, want nil when default branch is unknown", got.Code)
	}
}

func TestClientFetchRepository_EmptySelectionReturnsNilCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","default_branch":"main"}`))
		case "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case "/repos/owner/repo/readme":
			_, _ = w.Write([]byte("# README"))
		case "/repos/owner/repo/git/trees/main":
			_, _ = w.Write([]byte(`{"tree":[{"path":"node_modules/x.js","type":"blob","size":10},{"path":"package-lock.json","type":"blob","size":10}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Code != nil {
		t.Fatalf("Code = %#v, want nil when nothing is selected", got.Code)
	}
}

func TestClientFetchRepository_SkipsFailedFileFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","default_branch":"main"}`))
		case r.URL.Path == "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case r.URL.Path == "/repos/owner/repo/readme":
			_, _ = w.Write([]byte("# README"))
		case r.URL.Path == "/repos/owner/repo/git/trees/main":
			_, _ = w.Write([]byte(`{"tree":[{"path":"a.go","type":"blob","size":10},{"path":"b.go","type":"blob","size":10}]}`))
		case r.URL.Path == "/repos/owner/repo/contents/a.go":
			_, _ = w.Write([]byte("aaa"))
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if got.Code == nil || len(got.Code.Files) != 1 || got.Code.Files[0].Path != "a.go" {
		t.Fatalf("Code = %#v, want only a.go", got.Code)
	}
}

func TestClientFetchRepository_TruncatesOversizedREADME(t *testing.T) {
	oversized := strings.Repeat("A", maxREADMEBytes+1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			_, _ = w.Write([]byte(`{"full_name":"owner/repo","description":"desc","stargazers_count":1,"forks_count":1,"language":"Go"}`))
		case "/repos/owner/repo/languages":
			_, _ = w.Write([]byte(`{"Go":100}`))
		case "/repos/owner/repo/readme":
			_, _ = w.Write([]byte(oversized))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.Client(), server.URL, "")
	got, err := client.FetchRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("FetchRepository: %v", err)
	}
	if len(got.README) != maxREADMEBytes {
		t.Errorf("README length = %d, want %d", len(got.README), maxREADMEBytes)
	}
}

func TestNewGitHubClient_NilClient(t *testing.T) {
	client := NewGitHubClient(nil, "https://api.github.com", "")
	if client.httpClient == nil {
		t.Errorf("expected httpClient to be defaulted, but got nil")
	}
}
