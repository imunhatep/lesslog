// Package level classifies log lines by severity: it parses the many
// spellings of a level and guesses one from plain-text lines.
package level

import (
	"strings"
)

// Level is a log severity. Ordering matters: it drives the -min filter.
type Level int

const (
	Unknown Level = iota
	Trace
	Debug
	Info
	Warn
	Error
	Fatal
)

var levelNames = map[Level]string{
	Unknown: "-",
	Trace:   "trace",
	Debug:   "debug",
	Info:    "info",
	Warn:    "warn",
	Error:   "error",
	Fatal:   "fatal",
}

func (l Level) String() string { return levelNames[l] }

// Label is the fixed-width tag used in pretty mode.
func (l Level) Label() string {
	switch l {
	case Trace:
		return "TRC"
	case Debug:
		return "DBG"
	case Info:
		return "INF"
	case Warn:
		return "WRN"
	case Error:
		return "ERR"
	case Fatal:
		return "FTL"
	default:
		return "   "
	}
}

// Parse maps the many spellings of a level onto a Level.
func Parse(s string) Level {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "trace", "trc", "t", "verbose", "5":
		return Trace
	case "debug", "dbg", "d", "4":
		return Debug
	case "info", "information", "informational", "inf", "i", "notice", "3":
		return Info
	case "warn", "warning", "wrn", "w", "2":
		return Warn
	case "error", "err", "e", "1":
		return Error
	case "fatal", "ftl", "panic", "critical", "crit", "alert", "emerg", "emergency", "0":
		return Fatal
	}
	return Unknown
}

// Detect finds the level of a single log line without fully parsing it.
// JSON is handled by looking for the level key directly, then logfmt, then a
// bare word scan for plain-text logs.
func Detect(line, key string) Level {
	if v, ok := jsonStringField(line, key); ok {
		if l := Parse(v); l != Unknown {
			return l
		}
	}
	if v, ok := logfmtField(line, key); ok {
		if l := Parse(v); l != Unknown {
			return l
		}
	}
	return scanLevelWord(line)
}

// jsonStringField extracts a top-level-ish string value for "key" from a JSON
// object without allocating a map. It is a heuristic: good enough to colorize.
func jsonStringField(line, key string) (string, bool) {
	needle := `"` + key + `"`
	i := strings.Index(line, needle)
	if i < 0 {
		return "", false
	}
	j := i + len(needle)
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) || line[j] != ':' {
		return "", false
	}
	j++
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) || line[j] != '"' {
		return "", false
	}
	j++
	end := j
	for end < len(line) && line[end] != '"' {
		if line[end] == '\\' {
			end++
		}
		end++
	}
	if end > len(line) {
		end = len(line)
	}
	return line[j:end], true
}

// logfmtField extracts key=value / key="value" from a logfmt-ish line.
func logfmtField(line, key string) (string, bool) {
	needle := key + "="
	i := 0
	for {
		k := strings.Index(line[i:], needle)
		if k < 0 {
			return "", false
		}
		k += i
		// require a word boundary before the key
		if k == 0 || line[k-1] == ' ' || line[k-1] == '\t' || line[k-1] == '[' {
			v := line[k+len(needle):]
			if len(v) > 0 && v[0] == '"' {
				v = v[1:]
				if e := strings.IndexByte(v, '"'); e >= 0 {
					v = v[:e]
				}
				return v, true
			}
			if e := strings.IndexAny(v, " \t,]"); e >= 0 {
				v = v[:e]
			}
			return v, true
		}
		i = k + len(needle)
	}
}

// scanLevelWord looks for a standalone level word in a plain-text line,
// covering "2026-09-02 13:41 INFO starting", "[error] boom", "INFO[0000] up",
// zap's column layout "…13:41:13Z  info  caller  msg" and "panic: …".
//
// Prose must not decide the color of a line, so a candidate has to look like a
// tag rather than a word in a sentence: bracketed, written in upper case, or
// standing in its own column (surrounded by a tab, two spaces, a colon, or a
// line boundary). That rejects "An error occurred earlier" and "waiting for
// info from peer" while still catching real level fields.
func scanLevelWord(line string) Level {
	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		return Unknown // JSON without a level field: don't guess
	}
	head := line[:min(len(line), 160)]
	for i := 0; i < len(head); {
		if !isLetter(head[i]) {
			i++
			continue
		}
		end := i
		for end < len(head) && isLetter(head[end]) {
			end++
		}
		word := head[i:end]
		if len(word) >= 3 && len(word) <= 11 && looksLikeTag(head, i, end) {
			if l := Parse(word); l != Unknown {
				return l
			}
		}
		i = end
	}
	return Unknown
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// looksLikeTag reports whether head[start:end] is positioned like a level tag.
func looksLikeTag(head string, start, end int) bool {
	word := head[start:end]
	upper := word == strings.ToUpper(word)
	// mixed case ("Error" mid-sentence) is prose, never a tag
	if !upper && word != strings.ToLower(word) {
		return false
	}
	if start > 0 && strings.IndexByte("[(<|", head[start-1]) >= 0 {
		return true // [error], (warn), <info>
	}
	if upper {
		return true // WARN, ERROR: rare enough in prose to trust
	}
	return ownColumn(head, start, end)
}

// ownColumn reports whether the word is separated from its neighbours by more
// than a single space, i.e. it sits in a column of its own.
func ownColumn(head string, start, end int) bool {
	leftOK := start == 0 ||
		head[start-1] == '\t' ||
		(start >= 2 && head[start-1] == ' ' && head[start-2] == ' ')
	if !leftOK {
		return false
	}
	return end == len(head) ||
		head[end] == '\t' ||
		head[end] == ':' ||
		head[end] == ']' ||
		(head[end] == ' ' && (end+1 == len(head) || head[end+1] == ' '))
}
