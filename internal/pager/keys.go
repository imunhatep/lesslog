package pager

import (
	"os"
)

// Special keys are reported as bracketed names; everything else is the literal
// byte(s) typed (so "j" is "j" and ctrl-f is "\x06").
const (
	keyUp     = "<up>"
	keyDown   = "<down>"
	keyLeft   = "<left>"
	keyRight  = "<right>"
	keyPgUp   = "<pgup>"
	keyPgDn   = "<pgdn>"
	keyHome   = "<home>"
	keyEnd    = "<end>"
	keyEnter  = "<enter>"
	keyBS     = "<bs>"
	keyEsc    = "<esc>"
	keyDelete = "<del>"
)

// readKeys decodes terminal input into key strings until the tty closes.
func readKeys(f *os.File, out chan<- string) {
	defer close(out)
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			for _, k := range decodeKeys(buf[:n]) {
				out <- k
			}
		}
		if err != nil {
			return
		}
	}
}

func decodeKeys(b []byte) []string {
	var keys []string
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b && i+1 < len(b) && (b[i+1] == '[' || b[i+1] == 'O'):
			j := i + 2
			start := j
			for j < len(b) && !(b[j] >= '@' && b[j] <= '~') {
				j++
			}
			if j >= len(b) {
				keys = append(keys, keyEsc)
				i = len(b)
				continue
			}
			keys = append(keys, csiKey(string(b[start:j]), b[j]))
			i = j + 1
		case c == 0x1b:
			keys = append(keys, keyEsc)
			i++
		case c == '\r' || c == '\n':
			keys = append(keys, keyEnter)
			i++
		case c == 0x7f || c == 0x08:
			keys = append(keys, keyBS)
			i++
		default:
			// keep multi-byte UTF-8 runes together
			size := 1
			switch {
			case c >= 0xf0:
				size = 4
			case c >= 0xe0:
				size = 3
			case c >= 0xc0:
				size = 2
			}
			if i+size > len(b) {
				size = len(b) - i
			}
			keys = append(keys, string(b[i:i+size]))
			i += size
		}
	}
	return keys
}

func csiKey(params string, final byte) string {
	switch final {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		return keyRight
	case 'D':
		return keyLeft
	case 'H':
		return keyHome
	case 'F':
		return keyEnd
	case '~':
		switch params {
		case "1", "7":
			return keyHome
		case "3":
			return keyDelete
		case "4", "8":
			return keyEnd
		case "5":
			return keyPgUp
		case "6":
			return keyPgDn
		}
	}
	return "" // unknown sequence: ignored by the pager
}
