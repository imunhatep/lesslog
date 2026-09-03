// Package store reads the inputs in the background and keeps every line read
// so far together with the filtered view the pager shows.
package store

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
	"github.com/imunhatep/lesslog/internal/render"
)

// Store holds every line read so far plus the filtered view the pager shows.
// Everything (including the lazy pretty rendering of a line) happens under the
// mutex Lock exposes, so the loader goroutine and the pager can share lines
// safely.
type Store struct {
	mu   sync.Mutex
	opts *config.Options

	all  []*render.Line
	view []int // indexes into all that pass the current filters

	minLevel level.Level
	filter   *regexp.Regexp
	pretty   bool

	eof  bool
	errs []string

	notify chan struct{}
}

// New returns an empty store configured from o.
func New(o *config.Options) *Store {
	return &Store{
		opts:     o,
		minLevel: o.MinLevel,
		pretty:   o.Pretty,
		notify:   make(chan struct{}, 1),
	}
}

// Lock and Unlock guard the store. A reader must hold the lock for as long as
// it uses the lines it got from LineAt, because rendering one fills in its
// cached pretty form.
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// Notify returns the channel pinged whenever lines or errors are added.
func (s *Store) Notify() <-chan struct{} { return s.notify }

// ViewLen is the number of visible lines. The caller must hold the lock.
func (s *Store) ViewLen() int { return len(s.view) }

// LineAt returns the vi'th visible line, 0 <= vi < ViewLen. The caller must
// hold the lock.
func (s *Store) LineAt(vi int) *render.Line { return s.all[s.view[vi]] }

// Total is the number of lines read, visible or filtered out. The caller must
// hold the lock.
func (s *Store) Total() int { return len(s.all) }

// MinLevel is the level filter currently in effect. The caller must hold the
// lock.
func (s *Store) MinLevel() level.Level { return s.minLevel }

// EOF reports whether every source has been read to the end. The caller must
// hold the lock.
func (s *Store) EOF() bool { return s.eof }

// Errs returns the read errors collected so far, oldest first. The caller must
// hold the lock and must not modify the result.
func (s *Store) Errs() []string { return s.errs }

func (s *Store) ping() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Append adds a batch of freshly read lines and pings Notify.
func (s *Store) Append(batch []*render.Line) {
	s.mu.Lock()
	for _, l := range batch {
		s.all = append(s.all, l)
		if s.matches(l) {
			s.view = append(s.view, len(s.all)-1)
		}
	}
	s.mu.Unlock()
	s.ping()
}

// matches must be called with the store locked.
func (s *Store) matches(l *render.Line) bool {
	// Unknown-level lines (stack traces, plain output) are never hidden by the
	// level filter — they usually belong to the entry above them.
	if l.Level() != level.Unknown && l.Level() < s.minLevel {
		return false
	}
	if s.filter != nil && !s.filter.MatchString(l.Text(s.pretty, s.opts)) {
		return false
	}
	return true
}

// rebuild recomputes the view after a filter or mode change.
func (s *Store) rebuild() {
	s.view = s.view[:0]
	for i, l := range s.all {
		if s.matches(l) {
			s.view = append(s.view, i)
		}
	}
}

// SetMinLevel hides entries below l; level.Unknown shows everything.
func (s *Store) SetMinLevel(l level.Level) {
	s.mu.Lock()
	s.minLevel = l
	s.rebuild()
	s.mu.Unlock()
}

// SetFilter shows only the lines matching re; nil clears the filter.
func (s *Store) SetFilter(re *regexp.Regexp) {
	s.mu.Lock()
	s.filter = re
	s.rebuild()
	s.mu.Unlock()
}

// SetPretty tells the store which rendering the filter should match against.
func (s *Store) SetPretty(p bool) {
	s.mu.Lock()
	s.pretty = p
	if s.filter != nil {
		s.rebuild()
	}
	s.mu.Unlock()
}

// Count is ViewLen for callers that do not already hold the lock.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.view)
}

// AddErr records a read error to show on the status line.
func (s *Store) AddErr(msg string) {
	s.mu.Lock()
	s.errs = append(s.errs, msg)
	s.mu.Unlock()
	s.ping()
}

// A Source is one input to read, either a named file or stdin.
type Source struct {
	Name string // shown in error messages
	RC   io.ReadCloser
}

// Load reads all sources sequentially into the store. With follow set and a
// single seekable last source it keeps polling for appended data.
func (s *Store) Load(sources []Source, follow bool, done chan<- struct{}) {
	defer close(done)
	sanitizer := strings.NewReplacer("\t", "        ", "\r", "")

	for i, src := range sources {
		last := i == len(sources)-1
		r := bufio.NewReaderSize(src.RC, 256*1024)
		batch := make([]*render.Line, 0, 1024)
		flush := func() {
			if len(batch) > 0 {
				s.Append(batch)
				batch = make([]*render.Line, 0, 1024)
			}
		}
		tailing := follow && last
		pending := "" // partial last line, only possible while tailing
		for {
			raw, err := readLine(r)
			raw = pending + raw
			pending = ""
			// While tailing, a line without a newline is still being written:
			// hold it until the rest arrives.
			if err == io.EOF && tailing && raw != "" {
				pending = raw
			} else if raw != "" || err == nil {
				txt := stripANSI(sanitizer.Replace(raw))
				batch = append(batch, render.NewLine(txt, level.Detect(txt, s.opts.LevelKey)))
				if len(batch) >= 1024 {
					flush()
				}
			}
			if err != nil {
				flush()
				if err != io.EOF {
					s.AddErr(src.Name + ": " + err.Error())
					break
				}
				if !tailing {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
		flush()
		src.RC.Close()
	}
	s.mu.Lock()
	s.eof = true
	s.mu.Unlock()
	s.ping()
}

// readLine reads one line, returning it without the trailing newline. A final
// line without a newline is returned together with io.EOF.
func readLine(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, err := r.ReadString('\n')
		sb.WriteString(chunk)
		if err != nil {
			return strings.TrimSuffix(sb.String(), "\n"), err
		}
		if strings.HasSuffix(chunk, "\n") {
			return strings.TrimSuffix(sb.String(), "\n"), nil
		}
	}
}

// stripANSI removes escape sequences and stray control bytes so already
// colorized input cannot corrupt our own rendering.
func stripANSI(s string) string {
	if !strings.ContainsAny(s, "\x1b\x00") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\x1b' {
			// skip CSI/OSC style sequences
			j := i + 1
			if j < len(s) && (s[j] == '[' || s[j] == ']' || s[j] == '(') {
				j++
				for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
					j++
				}
			}
			i = j
			continue
		}
		if c < 0x20 && c != '\t' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
