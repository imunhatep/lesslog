package level

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		line string
		want Level
	}{
		{`{"level":"info","time":"2026-09-02T13:41:12Z","message":"logging level: trace"}`, Info},
		{`{"time":"2026-09-02T13:41:12Z","level":"trace","message":"sending request"}`, Trace},
		{`{"level": "warning" ,"message":"x"}`, Warn},
		{`{"level":"ERROR","message":"x"}`, Error},
		{`{"level":"panic","message":"x"}`, Fatal},
		{`{"message":"level is info-ish","other":1}`, Unknown},
		{`level=debug msg="hello"`, Debug},
		{`2026-09-02T13:41:20Z WARN  plain text line`, Warn},
		{`[error] nginx: upstream timed out`, Error},
		{"\tmain.main()", Unknown},
		{`{"level":"info","message":"escaped \" quote"}`, Info},
	}
	for _, c := range cases {
		if got := Detect(c.line, "level"); got != c.want {
			t.Errorf("Detect(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestDetectPlainText(t *testing.T) {
	cases := []struct {
		line string
		want Level
	}{
		// real level tags
		{`2026-09-02T13:41:13Z WARN  plain text line`, Warn},
		{`[error] nginx: upstream timed out`, Error},
		{`INFO[0000] logrus text line                    component=api`, Info},
		{"2026-09-02T13:41:13.101Z\tinfo\tcontroller\tstarting", Info},
		{"2026-09-02T13:41:13.101Z    info    controller    starting", Info},
		{`panic: runtime error: invalid memory address`, Fatal},
		{`error: could not open config`, Error},
		{`time=2026-09-02T13:41:14Z level=debug msg="hi"`, Debug},
		// prose must not be colored
		{`An error occurred earlier, see above`, Unknown},
		{`Waiting for info from peer`, Unknown},
		{`the request failed with a fatal problem downstream`, Unknown},
		{`Error while loading the file`, Unknown},
		// non-log noise
		{`goroutine 1 [running]:`, Unknown},
		{"\tmain.(*Controller).Run(0x14000122000)", Unknown},
		{`} orphaned closing brace`, Unknown},
		{``, Unknown},
		// JSON without a level field is never guessed at
		{`{"not_a_log":true,"msg":"an error occurred"}`, Unknown},
		// broken JSON still gets its level from the substring scan
		{`{"level":"info","message":"truncated`, Info},
	}
	for _, c := range cases {
		if got := Detect(c.line, "level"); got != c.want {
			t.Errorf("Detect(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	if got := Parse(" WARNING "); got != Warn {
		t.Errorf("Parse(%q) = %v, want %v", " WARNING ", got, Warn)
	}
	if got := Parse("nonsense"); got != Unknown {
		t.Errorf("Parse(%q) = %v, want %v", "nonsense", got, Unknown)
	}
}
