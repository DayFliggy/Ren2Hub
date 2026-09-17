package helper

import (
	"bytes"
	"fmt"
	"strings"
)

type sseEvent struct {
	data string
	name string
}

// sseDecoder joins data fields until the event boundary. Limits apply to the
// complete payload as well as individual scanner lines.
type sseDecoder struct {
	data    strings.Builder
	name    string
	hasData bool
	maxSize int
	started bool
}

func (d *sseDecoder) addLine(line string) (*sseEvent, error) {
	if !d.started {
		line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		d.started = true
	}
	if line == "" {
		return d.finish(), nil
	}
	if strings.HasPrefix(line, ":") {
		return nil, nil
	}
	field, value, _ := strings.Cut(line, ":")
	value = strings.TrimPrefix(value, " ")
	switch field {
	case "event":
		d.name = value
	case "data":
		extra := len(value)
		if d.hasData {
			extra++
		}
		if extra > d.maxSize-d.data.Len() {
			return nil, fmt.Errorf("SSE event exceeds %d bytes", d.maxSize)
		}
		if d.hasData {
			d.data.WriteByte('\n')
		}
		d.data.WriteString(value)
		d.hasData = true
	}
	return nil, nil
}

func (d *sseDecoder) finish() *sseEvent {
	var event *sseEvent
	if d.hasData {
		event = &sseEvent{data: d.data.String(), name: d.name}
	}
	d.data.Reset()
	d.name = ""
	d.hasData = false
	return event
}

// SSE permits CRLF, LF and CR line endings. A CR at a buffer boundary waits for
// the next read so CRLF is consumed as one delimiter.
func splitSSELine(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' {
			if i+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
		}
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
