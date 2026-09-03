# lesslog

A `less`-like pager for structured logs. Every line is colored by its log level,
and JSON lines are reformatted into something readable — while `p` always gets
you back to the raw bytes.

```
lesslog app.log
kubectl logs -f deploy/api | lesslog -f
lesslog -min warn -raw app.log other.log
```

Colors: `trace` grey, `debug` cyan, `info` green, `warn` yellow, `error` red,
`fatal`/`panic` white on red. In pretty mode the level color paints the
timestamp and the level tag; the message is bold white, parameter names are
italic blue and their values plain white, and a dim `›` sets the parameters off
from the message, so the text stays readable and the severity still reads at a
glance. Raw lines — anything that is not a JSON object — are
painted in the level color end to end, since they have no tag to carry it.
Lines with no recognizable level keep the terminal's default color and are
never hidden by the level filter — they are usually stack traces belonging to
the entry above them.

## Keys

| key | action |
| --- | --- |
| `j` `k` `↓` `↑` `enter` | one line |
| `space` `f` `b` `PgDn` `PgUp` | one page |
| `d` `u` | half a page |
| `g` `G` `<` `>` `Home` `End` | first / last line |
| `←` `→` | scroll sideways when long lines are chopped |
| `/pat` `?pat` `n` `N` | search forward / backward, next / previous match |
| `&pat` | show only matching lines (empty pattern clears the filter) |
| `l` `L` | raise / lower the minimum level shown |
| `p` | pretty ⇄ raw JSON |
| `w` | wrap ⇄ chop long lines |
| `F` | follow new output; any movement key stops following |
| `r` `ctrl-l` | redraw |
| `h` | help |
| `q` `ctrl-c` | quit |

Patterns are Go regexps with smartcase: an all-lowercase pattern matches
case-insensitively, anything with an upper-case letter matches exactly.

## Flags

```
-level-key string   JSON field holding the log level (default "level")
-msg-key string     fields holding the message (default "message,msg")
-time-key string    fields holding the timestamp (default "time,ts,timestamp,@timestamp")
-time short|full|none   timestamp rendering in pretty mode (default "short",
                    rendered as 2026-09-02 13:41:19.000)
-raw                start in raw mode instead of pretty-printing JSON
-S                  chop long lines instead of wrapping (default true)
-min level          hide levels below this one: trace|debug|info|warn|error
-f                  follow the input, like less +F / tail -f
-color auto|always|never
-no-pager           colorize straight to stdout instead of paging
```

## Pretty mode

```
{"level":"error","component":"controller","err":"context deadline exceeded",
 "tags":["provider","dns"],"time":"2026-09-02T13:41:19Z","message":"bulk export failed"}
```

becomes

```
2026-09-02 13:41:19.000 ERR  bulk export failed › component=controller err="context deadline exceeded" tags=["provider","dns"]
```

Field order is preserved and nested objects and arrays are kept as compact
JSON.

## Non-JSON lines

Mixed input is the normal case — a panic in the middle of a JSON stream, a
sidecar that logs plain text, a `kubectl` banner — so anything that is not a
JSON object is printed **verbatim**, including leading whitespace, and is never
dropped. Only its color is guessed at, in this order:

1. the JSON `level` field (`-level-key`), found by substring so even a
   truncated line still gets its color;
2. `level=` in a logfmt-ish line;
3. a level word that is positioned like a tag: bracketed (`[error] …`), in
   upper case (`… WARN msg`, `INFO[0000] …`), or standing in its own column
   (zap's tab/multi-space layout, `panic: …`).

Prose is deliberately left alone: `An error occurred earlier`, `Waiting for
info from peer` and `Error while loading` all stay in the terminal's default
color, because a level word in a sentence should not repaint the whole line.
Lines with no detected level are also exempt from the `-min` filter — a stack
trace belongs with the entry above it — and the status line reports how many
lines the filter is hiding.

A valid JSON object with no level field (`{"not_a_log":true}`) is still
reformatted as `key=value`, but it gets no level color and no empty level
column.

## Notes

- Input is read in the background, so opening a huge file is instant; the status
  line shows `loading` until it has all been read.
- Existing ANSI escapes in the input are stripped so pre-colored logs cannot
  corrupt the display.
- With `-no-pager`, or when stdout is not a terminal, lesslog just writes
  colorized lines through — `lesslog app.log | grep -i timeout` keeps working.

## Layout

The tree follows the usual Go application layout: a thin command under `cmd/`
and everything else under `internal/`, so the packages can be reshaped freely
without promising anything to importers outside this module.

```
cmd/lesslog/     main(): stamps the version and calls internal/cli
internal/cli/     flags, input files, and the choice of pager vs plain dump
internal/config/  Options: the resolved settings every package reads
internal/level/   log severities: parsing them, and guessing them from a line
internal/render/  line text, style spans, pretty-printing — no terminal I/O
internal/store/   background reader; all lines read plus the filtered view
internal/pager/   the interactive view: owns the terminal, draws, reads keys
testdata/         sample.log, used by `make demo`
```

Imports only ever point downwards through the layers `cli`, `pager`, `store`,
`render`, `level`, with `config` at the bottom, so there are no cycles and each
package can be read on its own. There is no `pkg/` directory: nothing here is
meant to be imported from outside yet, and adding one would freeze an API
before it has a consumer.

## Build

```
make              # build ./bin/lesslog
make install      # into $(PREFIX)/bin, PREFIX defaults to ~/.local
make check        # gofmt check + go vet + go test
make race         # tests under the race detector
make cover        # coverage summary
make demo         # page testdata/sample.log
make dist         # cross-build build/lesslog-<os>-<arch>
make help         # list every target
```

`VERSION` comes from `git describe` when available and is linked into the
binary, so `lesslog -version` reports the build it came from.

Straight from the module path, without cloning:

```
go install github.com/imunhatep/lesslog/cmd/lesslog@latest
```

## CI

`.github/workflows/build.yml` runs gofmt, `go vet` and the race-enabled tests
on every push and pull request, then cross-builds static binaries for
`linux/amd64`, `linux/arm64` and `darwin/arm64` and uploads each one, with its
`.sha256`, as a workflow artifact named `lesslog-<os>-<arch>`.
