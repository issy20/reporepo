package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/issy20/reporepo/internal/core"
)

type fakeStore struct {
	entries []*core.Entry
	loadErr error
}

func TestNewModelLoadsHistoryNewestFirstAndSkipsNil(t *testing.T) {
	old := &core.Entry{FullName: "old/repo", ViewedAt: time.Unix(1, 0)}
	newest := &core.Entry{FullName: "new/repo", ViewedAt: time.Unix(2, 0)}
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{old, nil, newest}}}, nil)

	if len(m.entries) != 2 || m.entries[0] != newest || m.entries[1] != old {
		t.Fatalf("entries = %#v, want newest first without nil", m.entries)
	}
}

func TestNewModelKeepsLoadErrorForUser(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{loadErr: errors.New("broken")}}, nil)
	if !strings.Contains(m.errMessage, "履歴を読み込めません") {
		t.Fatalf("errMessage = %q", m.errMessage)
	}
}

func TestRefreshVisibleFiltersFavoritesAndClampsSelection(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{entries: []*core.Entry{
		{FullName: "a/a"}, {FullName: "b/b", IsFavorite: true},
	}}}, nil)
	m.selected = 8
	m.tab = tabFavorites
	m.refreshVisible()

	if len(m.visible) != 1 || m.visible[0].FullName != "b/b" || m.selected != 0 {
		t.Fatalf("visible=%#v selected=%d", m.visible, m.selected)
	}
}

func (s *fakeStore) Load() ([]*core.Entry, error) { return s.entries, s.loadErr }
func (s *fakeStore) Save(entries []*core.Entry) error {
	s.entries = entries
	return nil
}
func (s *fakeStore) Upsert(entry *core.Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}

func TestNewModelUsesDefaults(t *testing.T) {
	m := NewModel(Dependencies{Store: &fakeStore{}}, &core.Config{})

	if m.state != stateInput {
		t.Fatalf("state = %v, want stateInput", m.state)
	}
	if m.language != "ja" {
		t.Errorf("language = %q, want ja", m.language)
	}
	if m.provider != "claude" {
		t.Errorf("provider = %q, want claude", m.provider)
	}
}
