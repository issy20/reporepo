package presentation

import (
	"errors"
	"io"
	"testing"
)

func TestCapabilityResolver(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tty       bool
		env       map[string]string
		decorated bool
	}{
		{"tty", true, nil, true}, {"non-tty", false, nil, false},
		{"no-color", true, map[string]string{"NO_COLOR": ""}, false},
		{"dumb", true, map[string]string{"TERM": "dumb"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := capabilityResolver{
				isTerminal: func(io.Writer) bool { return tc.tty },
				lookupEnv:  func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok },
				width:      func(io.Writer) (int, error) { return 120, nil },
			}
			got := r.resolve(io.Discard)
			if got.Decorated != tc.decorated || got.Width != 120 {
				t.Fatalf("resolve = %#v", got)
			}
		})
	}
}

func TestCapabilityResolverDefaultsWidth(t *testing.T) {
	r := capabilityResolver{isTerminal: func(io.Writer) bool { return true }, lookupEnv: func(string) (string, bool) { return "", false }, width: func(io.Writer) (int, error) { return 0, errors.New("no size") }}
	if got := r.resolve(io.Discard).Width; got != 80 {
		t.Fatalf("width = %d", got)
	}
}
