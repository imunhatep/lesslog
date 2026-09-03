// Package render turns log lines into the text and the style spans the
// screen is painted from. Nothing here writes to a terminal.
package render

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
)

// A Style is the body of an SGR escape, e.g. "1;31". Empty means default.
type Style string

const (
	// StyleDim is the grey of punctuation, separators and filler.
	StyleDim Style = "38;5;244"
	// StyleStatus is the inverted style of the status line.
	StyleStatus Style = "7"
	// styleKey is the parameter name: italic, slightly blue.
	styleKey  Style = "3;38;5;111"
	styleTime Style = "38;5;102"
	// styleText is the default color of message text: bold white.
	styleText Style = "1;97"
	// styleValue is the color of a parameter value: plain white, never bold, so
	// the message text stays the brightest thing on the line.
	styleValue Style = "97"
)

const (
	// reset returns the terminal to its default attributes.
	reset = "\x1b[0m"
	// sepParams separates the message text from the key=value parameters.
	sepParams = " › "
)

// LevelStyle is the base style applied to the whole line.
func LevelStyle(l level.Level) Style {
	switch l {
	case level.Trace:
		return "38;5;243"
	case level.Debug:
		return "36"
	case level.Info:
		return "32"
	case level.Warn:
		return "33"
	case level.Error:
		return "1;31"
	case level.Fatal:
		return "1;97;41"
	default:
		return ""
	}
}

// A Span applies a style to plain[Start:End) (rune offsets).
type Span struct {
	Start, End int
	Style      Style
}

// A Line is one input line plus its lazily built pretty rendering.
type Line struct {
	raw   string
	lvl   level.Level
	pret  string
	spans []Span
	json  bool // the line was parsed as a JSON object
	done  bool
}

// NewLine wraps one already sanitized input line and its detected level.
func NewLine(raw string, lvl level.Level) *Line {
	return &Line{raw: raw, lvl: lvl}
}

// Raw returns the line as it was read.
func (l *Line) Raw() string { return l.raw }

// Level returns the level detected for the line.
func (l *Line) Level() level.Level { return l.lvl }

// Text returns the display text for the current mode (never contains escapes).
func (l *Line) Text(pretty bool, o *config.Options) string {
	if !pretty {
		return l.raw
	}
	l.ensurePretty(o)
	return l.pret
}

// Styles returns the extra spans for the current mode.
func (l *Line) Styles(pretty bool, o *config.Options) []Span {
	if !pretty {
		return nil
	}
	l.ensurePretty(o)
	return l.spans
}

func (l *Line) ensurePretty(o *config.Options) {
	if l.done {
		return
	}
	l.done = true
	l.pret, l.spans, l.json = Pretty(l.raw, l.lvl, o)
}

// Base is the style the line's unstyled text falls back to. Pretty-printed
// entries carry their severity in the level tag, so their text stays bold
// white; raw lines have no tag and keep the level color instead.
func (l *Line) Base(pretty bool, o *config.Options) Style {
	if !pretty {
		return LevelStyle(l.lvl)
	}
	l.ensurePretty(o)
	if l.json {
		return styleText
	}
	return LevelStyle(l.lvl)
}

// Pretty turns a JSON log line into "time LVL message › key=value ..." and
// returns the plain text, the style spans for its parts, and whether the line
// was JSON at all. Non-JSON lines are returned unchanged.
func Pretty(raw string, lvl level.Level, o *config.Options) (string, []Span, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return raw, nil, false
	}
	fields, ok := parseOrdered([]byte(trimmed))
	if !ok {
		return raw, nil, false
	}

	var (
		b     strings.Builder
		spans []Span
		n     int // rune count written so far
	)
	write := func(s string, st Style) {
		if s == "" {
			return
		}
		b.WriteString(s)
		l := len([]rune(s))
		if st != "" {
			spans = append(spans, Span{Start: n, End: n + l, Style: st})
		}
		n += l
	}

	var ts, msg string
	rest := make([]kv, 0, len(fields))
	for _, f := range fields {
		switch {
		case f.key == o.LevelKey:
			// level is shown as the tag
		case ts == "" && slices.Contains(o.TimeKeys, f.key):
			ts = f.val
		case msg == "" && slices.Contains(o.MsgKeys, f.key):
			msg = f.val
		default:
			rest = append(rest, f)
		}
	}

	wroteTime := false
	if o.TimeMode != "none" && ts != "" {
		// The timestamp carries the level color too; levelless entries fall
		// back to grey so they stay out of the way.
		tsStyle := LevelStyle(lvl)
		if tsStyle == "" {
			tsStyle = styleTime
		}
		write(FormatTime(ts, o.TimeMode), tsStyle)
		wroteTime = true
	}
	// A JSON object with no level and no timestamp is probably not a log entry
	// at all, so don't indent it under an empty level column.
	if lvl != level.Unknown || wroteTime {
		if wroteTime {
			write(" ", "")
		}
		write(lvl.Label(), boldLevelStyle(lvl))
	}
	if msg != "" {
		if n > 0 {
			write("  ", "")
		}
		write(msg, "")
	}
	for i, f := range rest {
		switch {
		case i == 0 && msg != "":
			write(sepParams, StyleDim)
		case n > 0:
			write(" ", "")
		}
		write(f.key, styleKey)
		write("=", StyleDim)
		write(displayValue(f), styleValue)
	}
	return b.String(), spans, true
}

// FormatTime normalizes RFC3339-ish timestamps to
// "2006-01-02 15:04:05.000": the zone marker is dropped and the fractional
// part is always shown with millisecond precision. Timestamps that don't look
// like RFC3339 (epoch seconds, say) are returned as they came.
func FormatTime(ts, mode string) string {
	if mode == "full" {
		return ts
	}
	i := strings.IndexByte(ts, 'T')
	if i < 0 || len(ts) <= i+1 {
		return ts
	}
	date, clock := ts[:i], ts[i+1:]
	clock = strings.TrimSuffix(clock, "Z")
	if j := strings.IndexAny(clock, "+-"); j > 0 {
		clock = clock[:j]
	}
	frac := ""
	if j := strings.IndexByte(clock, '.'); j > 0 {
		frac, clock = clock[j+1:], clock[:j]
	}
	if len(frac) > 3 {
		frac = frac[:3]
	}
	return date + " " + clock + "." + frac + strings.Repeat("0", 3-len(frac))
}

func displayValue(f kv) string {
	if !f.quoted {
		return f.val
	}
	if f.val == "" || strings.ContainsAny(f.val, " \t\"") {
		return strconv.Quote(f.val)
	}
	return f.val
}

// kv is one JSON object member, in input order.
type kv struct {
	key    string
	val    string
	quoted bool // the value was a JSON string
}

// parseOrdered decodes a JSON object preserving field order. Nested values are
// re-encoded compactly.
func parseOrdered(data []byte) ([]kv, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	var out []kv
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := kt.(string)
		if !ok {
			return nil, false
		}
		val, quoted, err := readValue(dec)
		if err != nil {
			return nil, false
		}
		out = append(out, kv{key: key, val: val, quoted: quoted})
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, false
	}
	return out, true
}

func readValue(dec *json.Decoder) (string, bool, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", false, err
	}
	switch v := tok.(type) {
	case json.Delim:
		var b strings.Builder
		switch v {
		case '{':
			b.WriteByte('{')
			for i := 0; dec.More(); i++ {
				kt, err := dec.Token()
				if err != nil {
					return "", false, err
				}
				key, _ := kt.(string)
				val, quoted, err := readValue(dec)
				if err != nil {
					return "", false, err
				}
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strconv.Quote(key))
				b.WriteByte(':')
				b.WriteString(encodeNested(val, quoted))
			}
			if _, err := dec.Token(); err != nil {
				return "", false, err
			}
			b.WriteByte('}')
		case '[':
			b.WriteByte('[')
			for i := 0; dec.More(); i++ {
				val, quoted, err := readValue(dec)
				if err != nil {
					return "", false, err
				}
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(encodeNested(val, quoted))
			}
			if _, err := dec.Token(); err != nil {
				return "", false, err
			}
			b.WriteByte(']')
		}
		return b.String(), false, nil
	case string:
		return v, true, nil
	case json.Number:
		return v.String(), false, nil
	case bool:
		return strconv.FormatBool(v), false, nil
	default: // nil
		return "null", false, nil
	}
}

func encodeNested(val string, quoted bool) string {
	if quoted {
		return strconv.Quote(val)
	}
	return val
}

// Row paints one screen row: base level color, structural spans on top,
// then search highlights on top of that. Offsets are in runes.
func Row(text []rune, from, width int, base Style, spans, hl []Span, colorize bool) string {
	from = min(from, len(text))
	to := min(from+width, len(text))
	visible := text[from:to]
	if !colorize {
		return string(visible)
	}

	// style per visible rune
	styles := make([]Style, len(visible))
	for i := range styles {
		styles[i] = base
	}
	for _, s := range spans {
		for i := max(s.Start, from); i < min(s.End, to); i++ {
			styles[i-from] = s.Style
		}
	}
	for _, s := range hl {
		for i := max(s.Start, from); i < min(s.End, to); i++ {
			styles[i-from] = styles[i-from] + ";7"
		}
	}

	var b strings.Builder
	cur := Style("\x00") // force first emit
	for i, r := range visible {
		if styles[i] != cur {
			cur = styles[i]
			b.WriteString(reset)
			if cur != "" {
				b.WriteString("\x1b[")
				b.WriteString(string(cur))
				b.WriteString("m")
			}
		}
		b.WriteRune(r)
	}
	b.WriteString(reset)
	return b.String()
}

func Styled(s string, st Style, colorize bool) string {
	if !colorize || st == "" {
		return s
	}
	return "\x1b[" + string(st) + "m" + s + reset
}

// boldLevelStyle is the level color with bold added, for the level tag.
func boldLevelStyle(l level.Level) Style {
	s := LevelStyle(l)
	if s == "" || strings.HasPrefix(string(s), "1;") {
		return s
	}
	return "1;" + s
}
