// Package pager is the interactive, less-like view: it owns the terminal
// while it runs, draws the store's visible lines and handles the keys.
package pager

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
	"github.com/imunhatep/lesslog/internal/render"
	"github.com/imunhatep/lesslog/internal/store"
)

// Run takes over tty, pages st until the user quits and restores the terminal
// on the way out. loaded is closed once every source has been read.
func Run(st *store.Store, o *config.Options, tty *os.File, loaded <-chan struct{}) error {
	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	restore := func() {
		term.Restore(int(tty.Fd()), state)
		tty.WriteString("\x1b[?25h\x1b[?1049l") // show cursor, leave alt screen
	}
	defer restore()

	// don't leave the terminal in raw mode if we are killed
	fatal := make(chan os.Signal, 1)
	signal.Notify(fatal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-fatal
		restore()
		os.Exit(130)
	}()
	tty.WriteString("\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l") // alt screen, hide cursor

	keys := make(chan string, 64)
	go readKeys(tty, keys)

	return newPager(st, o, tty, keys, loaded).run()
}

type pager struct {
	st  *store.Store
	o   *config.Options
	tty *os.File

	keys   <-chan string
	winch  chan os.Signal
	loaded <-chan struct{}

	w, h int

	top  int // first visible line, index into store.view
	hoff int // horizontal scroll, in runes

	wrap   bool
	pretty bool
	follow bool

	search     *regexp.Regexp
	searchBack bool
	filterSrc  string

	msg  string
	quit bool
}

// row is one screen row: a view index plus the rune offset it starts at.
type row struct {
	vi   int
	from int
}

func newPager(st *store.Store, o *config.Options, tty *os.File, keys <-chan string, loaded <-chan struct{}) *pager {
	p := &pager{
		st:     st,
		o:      o,
		tty:    tty,
		keys:   keys,
		loaded: loaded,
		winch:  make(chan os.Signal, 1),
		wrap:   !o.Truncate,
		pretty: o.Pretty,
		follow: o.Follow,
	}
	p.resize()
	signal.Notify(p.winch, syscall.SIGWINCH)
	return p
}

func (p *pager) resize() {
	w, h, err := term.GetSize(int(p.tty.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	p.w, p.h = w, h
}

func (p *pager) content() int {
	if p.h < 2 {
		return 1
	}
	return p.h - 1
}

func (p *pager) run() error {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	p.draw()
	for !p.quit {
		select {
		case k, ok := <-p.keys:
			if !ok {
				return nil
			}
			p.handle(k)
			p.draw()
		case <-p.st.Notify():
			p.draw()
		case <-p.winch:
			p.resize()
			p.draw()
		case <-tick.C:
			if p.follow {
				p.draw()
			}
		}
	}
	return nil
}

// ---------- geometry (all callers must hold st.mu) ----------

func (p *pager) lineAt(vi int) *render.Line {
	return p.st.LineAt(vi)
}

func (p *pager) runes(vi int) []rune {
	return []rune(p.lineAt(vi).Text(p.pretty, p.o))
}

func (p *pager) rowsFor(vi int) int {
	if !p.wrap {
		return 1
	}
	n := len(p.runes(vi))
	if n == 0 {
		return 1
	}
	return (n + p.w - 1) / p.w
}

func (p *pager) maxTop() int {
	n := p.st.ViewLen()
	if !p.wrap {
		return max(0, n-p.content())
	}
	rows, top := 0, n
	for i := n - 1; i >= 0; i-- {
		rows += p.rowsFor(i)
		if rows > p.content() {
			break
		}
		top = i
	}
	return min(top, max(0, n-1))
}

func (p *pager) clamp() {
	if mt := p.maxTop(); p.top > mt {
		p.top = mt
	}
	if p.top < 0 {
		p.top = 0
	}
	if !p.wrap {
		if p.hoff < 0 {
			p.hoff = 0
		}
	} else {
		p.hoff = 0
	}
}

func (p *pager) visibleRows() []row {
	var rows []row
	for vi := p.top; vi < p.st.ViewLen() && len(rows) < p.content(); vi++ {
		if !p.wrap {
			rows = append(rows, row{vi: vi, from: p.hoff})
			continue
		}
		n := len(p.runes(vi))
		for from := 0; ; from += p.w {
			rows = append(rows, row{vi: vi, from: from})
			if from+p.w >= n || len(rows) >= p.content() {
				break
			}
		}
	}
	return rows
}

// ---------- drawing ----------

func (p *pager) draw() {
	p.st.Lock()
	defer p.st.Unlock()

	p.clamp()
	if p.follow {
		p.top = p.maxTop()
	}

	var buf bytes.Buffer
	buf.WriteString("\x1b[?25l\x1b[H")

	rows := p.visibleRows()
	for _, r := range rows {
		l := p.lineAt(r.vi)
		text := []rune(l.Text(p.pretty, p.o))
		var hl []render.Span
		if p.search != nil {
			hl = matchSpans(string(text), p.search)
		}
		buf.WriteString(render.Row(text, r.from, p.w, l.Base(p.pretty, p.o), l.Styles(p.pretty, p.o), hl, p.o.Color))
		buf.WriteString("\x1b[K\r\n")
	}
	for i := len(rows); i < p.content(); i++ {
		buf.WriteString(render.Styled("~", render.StyleDim, p.o.Color))
		buf.WriteString("\x1b[K\r\n")
	}
	buf.WriteString("\x1b[K")
	buf.WriteString(p.statusLine(rows))
	p.tty.Write(buf.Bytes())
}

func (p *pager) statusLine(rows []row) string {
	if p.msg != "" {
		return render.Styled(fit(p.msg, p.w), render.StyleStatus, p.o.Color)
	}
	n := p.st.ViewLen()
	first, last := 0, 0
	if len(rows) > 0 {
		first = rows[0].vi + 1
		last = rows[len(rows)-1].vi + 1
	}
	pct := 100
	if n > 0 {
		pct = last * 100 / n
	}
	var flags []string
	if !p.pretty {
		flags = append(flags, "raw")
	}
	if !p.wrap {
		flags = append(flags, "chop")
	}
	if p.st.MinLevel() > level.Unknown {
		flags = append(flags, "min="+p.st.MinLevel().String())
	}
	if p.filterSrc != "" {
		flags = append(flags, "&"+p.filterSrc)
	}
	if hidden := p.st.Total() - n; hidden > 0 {
		flags = append(flags, fmt.Sprintf("%d hidden", hidden))
	}
	if p.follow {
		flags = append(flags, "FOLLOW")
	}
	if !p.st.EOF() {
		flags = append(flags, "loading")
	}
	if len(p.st.Errs()) > 0 {
		flags = append(flags, "err: "+p.st.Errs()[len(p.st.Errs())-1])
	}

	s := fmt.Sprintf("%s  %d-%d/%d  %d%%", p.o.Title, first, last, n, pct)
	if len(flags) > 0 {
		s += "  [" + strings.Join(flags, "] [") + "]"
	}
	s += "   h=help q=quit"
	return render.Styled(fit(s, p.w), render.StyleStatus, p.o.Color)
}

func fit(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:max(w, 0)])
		}
		return string(r[:w-1]) + ">"
	}
	return s + strings.Repeat(" ", w-len(r))
}

func matchSpans(text string, re *regexp.Regexp) []render.Span {
	locs := re.FindAllStringIndex(text, -1)
	if locs == nil {
		return nil
	}
	// spans are in rune offsets; byte offsets only differ for non-ASCII text
	spans := make([]render.Span, 0, len(locs))
	ascii := len(text) == utf8.RuneCountInString(text)
	for _, l := range locs {
		s := render.Span{Start: l[0], End: l[1]}
		if !ascii {
			s.Start = utf8.RuneCountInString(text[:l[0]])
			s.End = s.Start + utf8.RuneCountInString(text[l[0]:l[1]])
		}
		spans = append(spans, s)
	}
	return spans
}

// ---------- key handling ----------

func (p *pager) handle(k string) {
	p.msg = ""
	switch k {
	case "q", "Q", "\x03": // ctrl-c
		p.quit = true
	case "j", keyDown, keyEnter, "\x0e":
		p.move(1)
	case "k", keyUp, "\x10":
		p.move(-1)
	case " ", "f", "\x06", keyPgDn:
		p.move(p.content())
	case "b", "\x02", keyPgUp:
		p.move(-p.content())
	case "d", "\x04":
		p.move(p.content() / 2)
	case "u", "\x15":
		p.move(-p.content() / 2)
	case "g", "<", keyHome:
		p.follow = false
		p.top = 0
	case "G", ">", keyEnd:
		p.st.Lock()
		p.top = p.maxTop()
		p.st.Unlock()
	case keyRight:
		p.hoff += 8
	case keyLeft:
		p.hoff = max(0, p.hoff-8)
	case "w":
		p.wrap = !p.wrap
	case "p":
		p.pretty = !p.pretty
		p.st.SetPretty(p.pretty)
	case "F":
		p.follow = !p.follow
	case "l":
		p.cycleLevel(1)
	case "L":
		p.cycleLevel(-1)
	case "/":
		p.startSearch(false)
	case "?":
		p.startSearch(true)
	case "n":
		p.repeatSearch(false)
	case "N":
		p.repeatSearch(true)
	case "&":
		p.startFilter()
	case "h", "H":
		p.showHelp()
	case "r", "\x0c": // ctrl-l
		// redraw only
	}
}

func (p *pager) move(delta int) {
	if delta > 0 {
		p.follow = false
	}
	p.st.Lock()
	p.top += delta
	p.clamp()
	p.st.Unlock()
	if delta < 0 {
		p.follow = false
	}
}

func (p *pager) cycleLevel(dir int) {
	p.st.Lock()
	cur := p.st.MinLevel()
	p.st.Unlock()

	l := int(cur) + dir
	if l > int(level.Error) {
		l = int(level.Unknown)
	}
	if l < int(level.Unknown) {
		l = int(level.Error)
	}
	p.st.SetMinLevel(level.Level(l))
	if level.Level(l) == level.Unknown {
		p.msg = "showing all levels"
	} else {
		p.msg = "min level: " + level.Level(l).String()
	}
}

func (p *pager) startSearch(back bool) {
	prefix := "/"
	if back {
		prefix = "?"
	}
	pat, ok := p.prompt(prefix)
	if !ok {
		return
	}
	if pat == "" {
		p.search = nil
		return
	}
	re, err := compilePattern(pat)
	if err != nil {
		p.msg = "bad pattern: " + err.Error()
		return
	}
	p.search, p.searchBack = re, back
	p.jump(back)
}

func (p *pager) repeatSearch(reverse bool) {
	if p.search == nil {
		p.msg = "no previous search"
		return
	}
	back := p.searchBack
	if reverse {
		back = !back
	}
	p.jump(back)
}

// jump moves to the next line matching p.search, starting just past the line
// currently at the top of the screen.
func (p *pager) jump(back bool) {
	p.st.Lock()
	defer p.st.Unlock()
	start := p.top + 1
	if back {
		start = p.top - 1
	}
	step := 1
	if back {
		step = -1
	}
	for i := start; i >= 0 && i < p.st.ViewLen(); i += step {
		if p.search.MatchString(p.lineAt(i).Text(p.pretty, p.o)) {
			p.top = i
			p.follow = false
			p.clamp()
			return
		}
	}
	p.msg = "pattern not found"
}

func (p *pager) startFilter() {
	pat, ok := p.prompt("&")
	if !ok {
		return
	}
	if pat == "" {
		p.filterSrc = ""
		p.st.SetFilter(nil)
		p.msg = "filter cleared"
		return
	}
	re, err := compilePattern(pat)
	if err != nil {
		p.msg = "bad pattern: " + err.Error()
		return
	}
	p.filterSrc = pat
	p.st.SetFilter(re)
	p.top = 0
}

func compilePattern(pat string) (*regexp.Regexp, error) {
	if pat == strings.ToLower(pat) { // smartcase
		return regexp.Compile("(?i)" + pat)
	}
	return regexp.Compile(pat)
}

// prompt reads a line of input on the status row. Returns ok=false on ESC.
func (p *pager) prompt(prefix string) (string, bool) {
	var input []rune
	for {
		p.drawPrompt(prefix + string(input))
		k, ok := <-p.keys
		if !ok {
			return "", false
		}
		switch k {
		case keyEnter:
			return string(input), true
		case keyEsc, "\x03":
			return "", false
		case keyBS:
			if len(input) > 0 {
				input = input[:len(input)-1]
			} else {
				return "", false
			}
		case "\x15": // ctrl-u
			input = nil
		default:
			if len(k) > 0 && k[0] >= 0x20 {
				input = append(input, []rune(k)...)
			}
		}
	}
}

func (p *pager) drawPrompt(s string) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[K%s\x1b[?25h", p.h, fit(s, p.w-1))
	p.tty.Write(buf.Bytes())
}

var helpText = []string{
	"  lesslog — a less-like pager that colors lines by log level",
	"",
	"  Movement",
	"    j / down / enter     one line down        k / up        one line up",
	"    space / f / PgDn     one page down        b / PgUp      one page up",
	"    d                    half page down       u             half page up",
	"    g / <                first line           G / >         last line",
	"    left / right         scroll sideways (when long lines are chopped)",
	"",
	"  Searching and filtering",
	"    /pattern             search forward       ?pattern      search backward",
	"    n / N                next / previous match (regexp, smartcase)",
	"    &pattern             show only matching lines (empty pattern clears)",
	"    l / L                raise / lower the minimum level shown",
	"",
	"  Display",
	"    p                    toggle pretty / raw JSON",
	"    w                    toggle wrap / chop long lines",
	"    F                    follow new output (tail), any movement key stops it",
	"    r / ctrl-l           redraw",
	"    q                    quit",
	"",
	"  Level colors: trace=grey debug=cyan info=green warn=yellow error=red fatal=on red",
	"",
	"  press any key to return",
}

func (p *pager) showHelp() {
	var buf bytes.Buffer
	buf.WriteString("\x1b[H\x1b[2J")
	for i, l := range helpText {
		if i >= p.h-1 {
			break
		}
		buf.WriteString(fit(l, p.w))
		buf.WriteString("\r\n")
	}
	p.tty.Write(buf.Bytes())
	<-p.keys
}
