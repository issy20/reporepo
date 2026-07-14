package clients

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestNewGitHubClient_NilClient(t *testing.T) {
	client := NewGitHubClient(nil, "https://api.github.com", "")
	if client.httpClient == nil {
		t.Errorf("expected httpClient to be defaulted, but got nil")
	}
}
