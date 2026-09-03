package pager

import (
	"regexp"
	"testing"

	"github.com/imunhatep/lesslog/internal/render"
)

func TestMatchSpans(t *testing.T) {
	re := regexp.MustCompile("ab")
	got := matchSpans("xxabxxab", re)
	want := []render.Span{{Start: 2, End: 4}, {Start: 6, End: 8}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("matchSpans = %v, want %v", got, want)
	}
	// non-ASCII: offsets must be in runes, not bytes
	got = matchSpans("héllo ab", re)
	if len(got) != 1 || got[0].Start != 6 || got[0].End != 8 {
		t.Errorf("matchSpans non-ascii = %v, want [{6 8}]", got)
	}
}

func TestDecodeKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"j", []string{"j"}},
		{"\x1b[A", []string{keyUp}},
		{"\x1b[6~", []string{keyPgDn}},
		{"\x1bOB", []string{keyDown}},
		{"\x1b", []string{keyEsc}},
		{"\r", []string{keyEnter}},
		{"\x7f", []string{keyBS}},
		{"jk\x1b[Bq", []string{"j", "k", keyDown, "q"}},
		{"é", []string{"é"}},
	}
	for _, c := range cases {
		got := decodeKeys([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("decodeKeys(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("decodeKeys(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestCompilePatternSmartcase(t *testing.T) {
	re, err := compilePattern("error")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("ERROR") {
		t.Error("lowercase pattern should be case-insensitive")
	}
	re, err = compilePattern("Error")
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchString("ERROR") {
		t.Error("mixed-case pattern should be case-sensitive")
	}
}

func TestFit(t *testing.T) {
	if got := fit("abc", 5); got != "abc  " {
		t.Errorf("fit pads to width: %q", got)
	}
	if got := fit("abcdef", 4); got != "abc>" {
		t.Errorf("fit truncates with a marker: %q", got)
	}
}
