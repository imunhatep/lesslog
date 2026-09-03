// Package cli parses the command line, opens the inputs and hands them to
// either the interactive pager or the plain dump path.
package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/level"
	"github.com/imunhatep/lesslog/internal/pager"
	"github.com/imunhatep/lesslog/internal/store"
)

// Run executes one invocation: args are the arguments after the program name,
// version is the build stamp printed by -version.
func Run(version string, args []string) error {
	def := config.Default()
	fs := flag.NewFlagSet("lesslog", flag.ExitOnError)
	var (
		levelKey = fs.String("level-key", def.LevelKey, "JSON field holding the log level")
		msgKeys  = fs.String("msg-key", strings.Join(def.MsgKeys, ","), "comma-separated JSON fields holding the message")
		timeKeys = fs.String("time-key", strings.Join(def.TimeKeys, ","), "comma-separated JSON fields holding the timestamp")
		timeMode = fs.String("time", def.TimeMode, "timestamp in pretty mode: short (2006-01-02 15:04:05.000)|full|none")
		raw      = fs.Bool("raw", !def.Pretty, "start in raw mode instead of pretty-printing JSON")
		chop     = fs.Bool("S", def.Truncate, "chop long lines instead of wrapping")
		minLvl   = fs.String("min", "", "hide levels below this one: trace|debug|info|warn|error")
		follow   = fs.Bool("f", false, "follow the input like less +F / tail -f")
		colorFlg = fs.String("color", "auto", "colorize: auto|always|never")
		noPager  = fs.Bool("no-pager", false, "colorize straight to stdout instead of paging")
		showVer  = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() { usage(fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVer {
		fmt.Println("lesslog", version)
		return nil
	}

	o := &config.Options{
		LevelKey: *levelKey,
		MsgKeys:  splitList(*msgKeys),
		TimeKeys: splitList(*timeKeys),
		TimeMode: *timeMode,
		Pretty:   !*raw,
		Truncate: *chop,
		Follow:   *follow,
	}
	if *minLvl != "" {
		l := level.Parse(*minLvl)
		if l == level.Unknown {
			return fmt.Errorf("unknown -min level %q", *minLvl)
		}
		o.MinLevel = l
	}

	sources, title, err := openSources(fs.Args())
	if err != nil {
		return err
	}
	o.Title = title

	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	switch *colorFlg {
	case "always":
		o.Color = true
	case "never":
		o.Color = false
	case "auto":
		o.Color = stdoutTTY || !*noPager
	default:
		return fmt.Errorf("unknown -color value %q", *colorFlg)
	}

	// A pager needs a terminal to draw on and one to read keys from.
	tty, ttyErr := openTTY(stdoutTTY)
	if *noPager || ttyErr != nil {
		if ttyErr != nil && !*noPager {
			o.Color = o.Color && stdoutTTY
		}
		if tty != nil {
			tty.Close()
		}
		return dump(sources, o)
	}
	defer tty.Close()

	st, loaded := load(sources, o)
	return pager.Run(st, o, tty, loaded)
}

// load starts reading the sources in the background and returns the store
// together with the channel closed once every source has been read.
func load(sources []store.Source, o *config.Options) (*store.Store, <-chan struct{}) {
	st := store.New(o)
	done := make(chan struct{})
	go st.Load(sources, o.Follow, done)
	return st, done
}

func usage(fs *flag.FlagSet) {
	fmt.Fprintf(fs.Output(), `lesslog — view JSON logs like less, colored by log level

usage: lesslog [flags] [file ...]
       ... | lesslog [flags]

Keys: j/k arrows space b d u g G scroll, / ? n N search, & filter,
      l/L minimum level, p pretty/raw, w wrap/chop, F follow, h help, q quit.

flags:
`)
	fs.PrintDefaults()
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func openSources(args []string) ([]store.Source, string, error) {
	if len(args) == 0 {
		return []store.Source{{Name: "(stdin)", RC: os.Stdin}}, "(stdin)", nil
	}
	var sources []store.Source
	for _, a := range args {
		if a == "-" {
			sources = append(sources, store.Source{Name: "(stdin)", RC: os.Stdin})
			continue
		}
		f, err := os.Open(a)
		if err != nil {
			return nil, "", err
		}
		sources = append(sources, store.Source{Name: a, RC: f})
	}
	title := filepath.Base(args[0])
	if len(args) > 1 {
		title = fmt.Sprintf("%s (+%d more)", title, len(args)-1)
	}
	return sources, title, nil
}

// openTTY returns the terminal to page on: stdout if it is a terminal,
// otherwise /dev/tty (so `cmd | lesslog > file` still behaves sanely).
func openTTY(stdoutTTY bool) (*os.File, error) {
	if stdoutTTY {
		f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		// no controllable tty for keys; fall back to dumping
		return nil, err
	}
	return nil, fmt.Errorf("stdout is not a terminal")
}
