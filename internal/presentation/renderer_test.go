package presentation

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRendererSemanticMessages(t *testing.T) {
	for _, tc := range []struct {
		name, method, plainPrefix, decoratedPrefix string
	}{
		{"success", "success", "OK: ", "✓ "},
		{"warning", "warning", "WARNING: ", "⚠ "},
		{"error", "error", "ERROR: ", "✗ "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, decorated := range []bool{false, true} {
				var out bytes.Buffer
				r := NewRenderer(&out, Capabilities{Decorated: decorated, Width: 80})
				var err error
				switch tc.method {
				case "success":
					err = r.Success("完了")
				case "warning":
					err = r.Warning("注意")
				case "error":
					err = r.Error("失敗")
				}
				if err != nil {
					t.Fatal(err)
				}
				got := out.String()
				if decorated {
					if !strings.Contains(got, "\x1b[") || !strings.Contains(StripANSI(got), tc.decoratedPrefix) {
						t.Fatalf("decorated output = %q", got)
					}
				} else if strings.Contains(got, "\x1b[") || !strings.HasPrefix(got, tc.plainPrefix) {
					t.Fatalf("plain output = %q", got)
				}
			}
		})
	}
}

func TestRendererSummaryLayoutAndDoesNotTruncate(t *testing.T) {
	rows := []Row{{Label: "a", Value: "/a/very/long/path"}, {Label: "long", Value: "設定済み"}}
	for _, tc := range []struct {
		width int
		want  string
	}{{80, "a     /a/very/long/path"}, {20, "a: /a/very/long/path"}} {
		var out bytes.Buffer
		if err := NewRenderer(&out, Capabilities{Width: tc.width}).Summary(rows); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("width %d output = %q", tc.width, out.String())
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRendererReturnsWriterErrorAndEscapesInputANSI(t *testing.T) {
	if err := NewRenderer(failingWriter{}, Capabilities{}).Success("x"); err == nil {
		t.Fatal("writer error = nil")
	}
	var out bytes.Buffer
	if err := NewRenderer(&out, Capabilities{Decorated: true}).Success("safe\x1b[31munsafe"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(StripANSI(out.String()), "\x1b") {
		t.Fatalf("input escape was preserved: %q", out.String())
	}
}
