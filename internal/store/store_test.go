package store

import (
	"regexp"
	"strings"
	"testing"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
	"github.com/imunhatep/lesslog/internal/render"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m plain\x00"
	if got := stripANSI(in); got != "red plain" {
		t.Errorf("stripANSI = %q", got)
	}
}

func TestFilters(t *testing.T) {
	st := New(config.Default())
	lines := []string{
		`{"level":"trace","message":"a"}`,
		`{"level":"info","message":"b"}`,
		`{"level":"error","message":"c"}`,
		`no level here`,
	}
	batch := make([]*render.Line, 0, len(lines))
	for _, l := range lines {
		batch = append(batch, render.NewLine(l, level.Detect(l, "level")))
	}
	st.Append(batch)

	if got := st.Count(); got != 4 {
		t.Fatalf("count = %d, want 4", got)
	}
	st.SetMinLevel(level.Info)
	// info + error + the unknown line, which is never hidden
	if got := st.Count(); got != 3 {
		t.Errorf("count after min=info = %d, want 3", got)
	}
	st.SetMinLevel(level.Unknown)
	st.SetFilter(regexp.MustCompile("ERR"))
	if got := st.Count(); got != 1 {
		t.Errorf("count after filter = %d, want 1", got)
	}
	st.SetFilter(nil)
	if got := st.Count(); got != 4 {
		t.Errorf("count after clearing filter = %d, want 4", got)
	}
}

func TestLoadNoTrailingNewline(t *testing.T) {
	st := New(config.Default())
	done := make(chan struct{})
	src := Source{Name: "test", RC: nopCloser{strings.NewReader("a\nb\n\nc")}}
	st.Load([]Source{src}, false, done)
	<-done

	st.Lock()
	defer st.Unlock()
	if st.Total() != 4 {
		t.Fatalf("got %d lines, want 4", st.Total())
	}
	got := make([]string, st.ViewLen())
	for i := range got {
		got[i] = st.LineAt(i).Raw()
	}
	if want := []string{"a", "b", "", "c"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestLoadSetsEOF(t *testing.T) {
	st := New(config.Default())
	done := make(chan struct{})
	src := Source{Name: "empty", RC: nopCloser{strings.NewReader("")}}
	st.Load([]Source{src}, false, done)
	<-done

	st.Lock()
	defer st.Unlock()
	if !st.EOF() {
		t.Error("EOF should be set once every source is read")
	}
	if len(st.Errs()) != 0 {
		t.Errorf("unexpected errors: %q", st.Errs())
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
