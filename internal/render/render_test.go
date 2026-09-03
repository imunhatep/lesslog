package render

import (
	"strings"
	"testing"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
)

func TestPretty(t *testing.T) {
	o := config.Default()
	in := `{"level":"error","component":"controller","err":"context deadline exceeded","tags":["provider","dns"],"detail":{"code":504,"retryable":true},"time":"2026-09-02T13:41:19Z","message":"bulk export failed"}`
	got, spans, isJSON := Pretty(in, level.Error, o)
	if !isJSON {
		t.Error("expected the line to be recognized as JSON")
	}
	want := `2026-09-02 13:41:19.000 ERR  bulk export failed › component=controller err="context deadline exceeded" tags=["provider","dns"] detail={"code":504,"retryable":true}`
	if got != want {
		t.Errorf("pretty =\n%q\nwant\n%q", got, want)
	}
	if len(spans) == 0 {
		t.Error("expected style spans")
	}
	for _, s := range spans {
		if s.Start < 0 || s.End > len([]rune(got)) || s.Start >= s.End {
			t.Errorf("span %v out of range for %d runes", s, len([]rune(got)))
		}
	}
}

func TestPrettyPreservesNonJSON(t *testing.T) {
	o := config.Default()
	in := "goroutine 1 [running]:"
	if got, spans, isJSON := Pretty(in, level.Unknown, o); got != in || spans != nil || isJSON {
		t.Errorf("Pretty(%q) = %q, %v; want unchanged", in, got, spans)
	}
	broken := `{"level":"info","message":"truncated`
	if got, _, isJSON := Pretty(broken, level.Info, o); got != broken || isJSON {
		t.Errorf("broken JSON should pass through, got %q", got)
	}
}

func TestPrettyLevellessJSON(t *testing.T) {
	o := config.Default()
	got, spans, _ := Pretty(`{"not_a_log":true,"but_valid":"json"}`, level.Unknown, o)
	if want := `not_a_log=true but_valid=json`; got != want {
		t.Errorf("pretty = %q, want %q", got, want)
	}
	// an unknown level must not emit an empty "1;" SGR for its blank tag
	row := Row([]rune(got), 0, len([]rune(got)), LevelStyle(level.Unknown), spans, nil, true)
	if strings.Contains(row, "\x1b[1;m") {
		t.Errorf("malformed escape in %q", row)
	}
}

// Parameter values must not be bold, so that the message text stays the
// brightest part of the line.
func TestPrettyValuesNotBold(t *testing.T) {
	o := config.Default()
	got, spans, _ := Pretty(`{"level":"info","message":"hi","user":"svc-api"}`, level.Info, o)
	i := strings.Index(got, "svc-api")
	if i < 0 {
		t.Fatalf("value missing from %q", got)
	}
	start := len([]rune(got[:i]))
	for _, s := range spans {
		if s.Start == start {
			if s.Style != styleValue || strings.Contains(string(s.Style), "1;") {
				t.Errorf("value style = %q, want %q", s.Style, styleValue)
			}
			return
		}
	}
	t.Errorf("no span covers the value in %q", got)
}

func TestFormatTime(t *testing.T) {
	cases := []struct{ in, mode, want string }{
		{"2026-09-02T13:41:19Z", "short", "2026-09-02 13:41:19.000"},
		{"2026-09-02T13:41:19.123456Z", "short", "2026-09-02 13:41:19.123"},
		{"2026-09-02T13:41:19.5Z", "short", "2026-09-02 13:41:19.500"},
		{"2026-09-02T13:41:19+03:00", "short", "2026-09-02 13:41:19.000"},
		{"2026-09-02T13:41:19Z", "full", "2026-09-02T13:41:19Z"},
		{"1756819279", "short", "1756819279"},
	}
	for _, c := range cases {
		if got := FormatTime(c.in, c.mode); got != c.want {
			t.Errorf("FormatTime(%q, %q) = %q, want %q", c.in, c.mode, got, c.want)
		}
	}
}

func TestRowStyles(t *testing.T) {
	text := []rune("hello world")
	spans := []Span{{Start: 6, End: 11, Style: styleKey}}
	hl := []Span{{Start: 0, End: 5}}
	got := Row(text, 0, 11, "32", spans, hl, true)
	if !strings.Contains(got, "\x1b[32;7mhello") {
		t.Errorf("expected highlighted base-colored prefix, got %q", got)
	}
	if !strings.Contains(got, "\x1b["+string(styleKey)+"mworld") {
		t.Errorf("expected span style on suffix, got %q", got)
	}
	if !strings.HasSuffix(got, reset) {
		t.Errorf("expected trailing reset, got %q", got)
	}
	if plain := Row(text, 0, 11, "32", spans, hl, false); plain != "hello world" {
		t.Errorf("colorize=false must stay plain, got %q", plain)
	}
}

func TestRowWindow(t *testing.T) {
	text := []rune("0123456789")
	if got := Row(text, 3, 4, "", nil, nil, false); got != "3456" {
		t.Errorf("window = %q, want %q", got, "3456")
	}
	if got := Row(text, 20, 4, "", nil, nil, false); got != "" {
		t.Errorf("past end = %q, want empty", got)
	}
}

func TestLineModes(t *testing.T) {
	o := config.Default()
	l := NewLine(`{"level":"info","message":"hi"}`, level.Info)
	if l.Raw() != `{"level":"info","message":"hi"}` {
		t.Errorf("Raw = %q", l.Raw())
	}
	if l.Level() != level.Info {
		t.Errorf("Level = %v", l.Level())
	}
	if got := l.Text(false, o); got != l.Raw() {
		t.Errorf("raw mode text = %q, want the raw line", got)
	}
	if got := l.Text(true, o); !strings.HasSuffix(got, "INF  hi") {
		t.Errorf("pretty text = %q", got)
	}
	if got := l.Base(true, o); got != styleText {
		t.Errorf("pretty base = %q, want %q", got, styleText)
	}
	if got := l.Base(false, o); got != LevelStyle(level.Info) {
		t.Errorf("raw base = %q, want the level color", got)
	}
}
