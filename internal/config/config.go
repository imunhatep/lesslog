// Package config holds the settings every other package reads: which JSON
// fields carry the level, message and timestamp, and how the view starts out.
package config

import "github.com/imunhatep/lesslog/internal/level"

// Options is the resolved configuration for one run. It is written once during
// flag parsing and only read afterwards; the pager keeps its own copy of the
// settings the user can toggle at runtime.
type Options struct {
	LevelKey string   // JSON field holding the level
	MsgKeys  []string // JSON fields holding the message, in priority order
	TimeKeys []string // JSON fields holding the timestamp, in priority order
	TimeMode string   // short | full | none

	Pretty   bool
	Truncate bool // chop long lines instead of wrapping
	Follow   bool
	MinLevel level.Level
	Color    bool
	Title    string // shown on the status line
}

// Default returns the options the CLI starts from, so the flag defaults and
// the tests share one source of truth.
func Default() *Options {
	return &Options{
		LevelKey: "level",
		MsgKeys:  []string{"message", "msg"},
		TimeKeys: []string{"time", "ts", "timestamp", "@timestamp"},
		TimeMode: "short",
		Pretty:   true,
		Truncate: true,
	}
}
