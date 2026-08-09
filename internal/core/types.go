package core

import "time"

// Entry はストアに保存されるリポジトリごとの主レコード。
type Entry struct {
	FullName   string               `json:"full_name"`
	RepoMeta   *RepoMeta            `json:"repo_meta"`
	Analyses   map[string]*Analysis `json:"analyses"`
	IsFavorite bool                 `json:"is_favorite"`
	ViewedAt   time.Time            `json:"viewed_at"`
	CreatedAt  time.Time            `json:"created_at"`
}

// Analysis は言語別のAI生成結果。
type Analysis struct {
	Summary       string    `json:"summary"`
	TechStack     string    `json:"tech_stack"`
	Background    string    `json:"background"`
	Keywords      []string  `json:"keywords"`
	Language      string    `json:"language"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	PromptVersion int       `json:"prompt_version"`
	CreatedAt     time.Time `json:"created_at"`
}

// RepoMeta はGitHubから取得したリポジトリのメタ情報。
type RepoMeta struct {
	FullName    string         `json:"full_name"`
	Description string         `json:"description"`
	Stars       int            `json:"stars"`
	Forks       int            `json:"forks"`
	Language    string         `json:"language"`
	Topics      []string       `json:"topics"`
	Languages   map[string]int `json:"languages"`
	URL         string         `json:"url"`
	License     string         `json:"license"`
	UpdatedAt   time.Time      `json:"updated_at"`
	FetchedAt   time.Time      `json:"fetched_at"` // 最後にGitHubから取得した日時
}

// IsStale は解析がリポジトリの最終更新より前のものかを返す。
func (a *Analysis) IsStale(meta *RepoMeta) bool {
	if a == nil || meta == nil {
		return false
	}
	return a.CreatedAt.Before(meta.UpdatedAt)
}
