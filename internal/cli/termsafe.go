package cli

import (
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

type termWriter struct {
	f *os.File
}

func (w termWriter) Write(p []byte) (int, error) {
	if _, err := w.f.Write(sanitizeTerminal(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func termSafe(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return termWriter{f: f}
	}
	return w
}

func fileOf(w io.Writer) (*os.File, bool) {
	switch v := w.(type) {
	case *os.File:
		return v, true
	case termWriter:
		return v.f, true
	}
	return nil, false
}

func sgrEnd(p []byte, i int) int {
	if i+1 >= len(p) || p[i] != 0x1b || p[i+1] != '[' {
		return 0
	}
	for j := i + 2; j < len(p); j++ {
		c := p[j]
		if c == 'm' {
			return j + 1
		}
		if c != ';' && (c < '0' || c > '9') {
			return 0
		}
	}
	return 0
}

func sanitizeTerminal(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); {
		if end := sgrEnd(p, i); end > 0 {
			out = append(out, p[i:end]...)
			i = end
			continue
		}
		c := p[i]
		r, size := utf8.DecodeRune(p[i:])
		switch {
		case c == '\n' || c == '\t' || (c == '\r' && i+1 < len(p) && p[i+1] == '\n'):
			out = append(out, c)
		case c < 0x20:
			out = append(out, '^', c^0x40)
		case c == 0x7f:
			out = append(out, '^', '?')
		case (r >= 0x80 && r <= 0x9f) || (r == utf8.RuneError && size == 1):
			out = utf8.AppendRune(out, utf8.RuneError)
		default:
			out = append(out, p[i:i+size]...)
		}
		i += size
	}
	return out
}
