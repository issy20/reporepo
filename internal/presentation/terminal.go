package presentation

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const defaultWidth = 80

// Capabilities は出力先ごとの端末表示能力を表す。
type Capabilities struct {
	Decorated bool
	Width     int
}

type capabilityResolver struct {
	isTerminal func(io.Writer) bool
	lookupEnv  func(string) (string, bool)
	width      func(io.Writer) (int, error)
}

func (r capabilityResolver) resolve(out io.Writer) Capabilities {
	width, err := r.width(out)
	if err != nil || width <= 0 {
		width = defaultWidth
	}
	_, noColor := r.lookupEnv("NO_COLOR")
	termName, _ := r.lookupEnv("TERM")
	return Capabilities{
		Decorated: r.isTerminal(out) && !noColor && !strings.EqualFold(termName, "dumb"),
		Width:     width,
	}
}

// ResolveCapabilities はwriterと環境から表示能力を解決する。
func ResolveCapabilities(out io.Writer) Capabilities {
	resolver := capabilityResolver{
		isTerminal: func(w io.Writer) bool {
			file, ok := w.(*os.File)
			return ok && term.IsTerminal(int(file.Fd()))
		},
		lookupEnv: os.LookupEnv,
		width: func(w io.Writer) (int, error) {
			file, ok := w.(*os.File)
			if !ok {
				return 0, os.ErrInvalid
			}
			width, _, err := term.GetSize(int(file.Fd()))
			return width, err
		},
	}
	return resolver.resolve(out)
}
