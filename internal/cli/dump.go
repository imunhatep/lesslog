package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/imunhatep/lesslog/internal/config"
	"github.com/imunhatep/lesslog/internal/render"
	"github.com/imunhatep/lesslog/internal/store"
)

// dump colorizes to stdout without paging.
func dump(sources []store.Source, o *config.Options) error {
	st, done := load(sources, o)

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	next := 0
	emit := func() {
		st.Lock()
		for ; next < st.ViewLen(); next++ {
			l := st.LineAt(next)
			text := []rune(l.Text(o.Pretty, o))
			w.WriteString(render.Row(text, 0, len(text), render.LevelStyle(l.Level()), l.Styles(o.Pretty, o), nil, o.Color))
			w.WriteByte('\n')
		}
		st.Unlock()
		w.Flush()
	}
	for {
		select {
		case <-st.Notify():
			emit()
		case <-done:
			emit()
			st.Lock()
			errs := st.Errs()
			st.Unlock()
			if len(errs) > 0 {
				return fmt.Errorf("%s", strings.Join(errs, "; "))
			}
			return nil
		}
	}
}
