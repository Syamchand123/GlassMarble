package provider

import (
	"bufio"
	"io"
	"strings"
)

// scanSSE iterates a Server-Sent-Events stream and calls fn for each event
// block with its event type (may be "") and the joined data payload (which
// spans zero or more "data:" lines). Comment lines and keep-alives are
// skipped. Trailing \r is stripped so CRLF streams parse cleanly.
func scanSSE(r io.Reader, fn func(event, data string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var event, data string
	flush := func() {
		if data != "" || event != "" {
			fn(event, data)
		}
		event, data = "", ""
	}
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if v, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, "data:"); ok {
			v = strings.TrimSpace(v)
			if data != "" {
				data += "\n"
			}
			data += v
		}
	}
	flush()
}
