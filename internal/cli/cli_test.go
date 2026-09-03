package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitList(t *testing.T) {
	got := splitList(" message , msg ,, ")
	if want := []string{"message", "msg"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("splitList = %q, want %q", got, want)
	}
	if got := splitList(""); got != nil {
		t.Errorf("splitList(\"\") = %q, want nil", got)
	}
}

func TestOpenSources(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// no arguments: stdin
	srcs, title, err := openSources(nil)
	if err != nil || len(srcs) != 1 || title != "(stdin)" {
		t.Fatalf("openSources(nil) = %v, %q, %v", srcs, title, err)
	}

	srcs, title, err = openSources([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, s := range srcs {
			s.RC.Close()
		}
	}()
	if len(srcs) != 2 || srcs[0].Name != a {
		t.Errorf("sources = %v", srcs)
	}
	if want := "a.log (+1 more)"; title != want {
		t.Errorf("title = %q, want %q", title, want)
	}

	if _, _, err := openSources([]string{filepath.Join(dir, "missing.log")}); err == nil {
		t.Error("expected an error for a missing file")
	}
}
