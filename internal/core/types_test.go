package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRepoMetaFetchedAtMarshalsAndUnmarshals(t *testing.T) {
	fetched := time.Date(2026, 7, 14, 3, 4, 5, 0, time.UTC)
	data, err := json.Marshal(&RepoMeta{FetchedAt: fetched})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded RepoMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !decoded.FetchedAt.Equal(fetched) {
		t.Fatalf("FetchedAt = %v, want %v", decoded.FetchedAt, fetched)
	}
}

func TestExistingJSONWithZeroFetchedAtLoads(t *testing.T) {
	data := []byte(`{"full_name":"owner/repo","updated_at":"2026-07-14T00:00:00Z"}`)
	var meta RepoMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !meta.FetchedAt.IsZero() {
		t.Fatalf("FetchedAt = %v, want zero for legacy JSON", meta.FetchedAt)
	}
	if meta.FullName != "owner/repo" {
		t.Fatalf("FullName = %q", meta.FullName)
	}
}

func TestAnalysisIsStale(t *testing.T) {
	updated := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		analysis *Analysis
		meta     *RepoMeta
		want     bool
	}{
		{"created before updated", &Analysis{CreatedAt: created}, &RepoMeta{UpdatedAt: updated}, true},
		{"created same day as updated", &Analysis{CreatedAt: updated}, &RepoMeta{UpdatedAt: updated}, false},
		{"created after updated", &Analysis{CreatedAt: updated.Add(24 * time.Hour)}, &RepoMeta{UpdatedAt: updated}, false},
		{"zero updated", &Analysis{CreatedAt: created}, &RepoMeta{UpdatedAt: time.Time{}}, false},
		{"nil analysis", nil, &RepoMeta{UpdatedAt: updated}, false},
		{"nil meta", &Analysis{CreatedAt: created}, nil, false},
		{"nil analysis and nil meta", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.IsStale(tt.meta); got != tt.want {
				t.Fatalf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}
