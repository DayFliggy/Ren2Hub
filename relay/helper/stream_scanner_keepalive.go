package helper

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/tidwall/gjson"
)

// upstreamKeepAliveMarker tags the fake heartbeat some third-party relays send as
// a data event, e.g. {"type":"response.output_text.delta","item_id":"SSE-Keep-Alive",
// "delta":"","SSE-Keep-Alive":true}, before response.created. Clients that track
// output items strictly (Codex) reject a delta for an item that was never added.
const upstreamKeepAliveMarker = "SSE-Keep-Alive"

var upstreamKeepAliveMarkerBytes = []byte(upstreamKeepAliveMarker)

// IsUpstreamKeepAliveEvent matches only a bare heartbeat: the marker with no
// content. An event carrying a delta, item or response is real and passes through.
func IsUpstreamKeepAliveEvent(data string) bool {
	if !strings.Contains(data, upstreamKeepAliveMarker) || !gjson.Valid(data) {
		return false
	}
	event := gjson.Parse(data)
	if !event.Get(upstreamKeepAliveMarker).Bool() && event.Get("item_id").String() != upstreamKeepAliveMarker {
		return false
	}
	return event.Get("delta").String() == "" && !event.Get("item").Exists() && !event.Get("response").Exists()
}

func isKeepAliveSSELine(line []byte) bool {
	if !bytes.Contains(line, upstreamKeepAliveMarkerBytes) {
		return false
	}
	trimmed := strings.TrimRight(string(line), "\r\n")
	data, hasPrefix := strings.CutPrefix(trimmed, "data:")
	if !hasPrefix {
		return false
	}
	return IsUpstreamKeepAliveEvent(strings.TrimSpace(data))
}

func wrapKeepAliveFilterReader(reader io.Reader) io.Reader {
	if reader == nil || !operation_setting.GetGeneralSetting().DropUpstreamKeepAliveEnabled {
		return reader
	}
	return &keepAliveFilterReader{
		src: bufio.NewReaderSize(reader, 4096),
	}
}

// keepAliveFilterReader drops bare SSE-Keep-Alive data lines before the scanner
// sees them so fake heartbeats neither count as the first response nor reach
// strict clients. Lines longer than the 4KB read buffer stream straight through
// without buffering the full line.
type keepAliveFilterReader struct {
	src      *bufio.Reader
	pending  []byte
	err      error
	passLine bool
}

func (r *keepAliveFilterReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}

	for {
		frag, err := r.src.ReadSlice('\n')
		if r.passLine {
			if !errors.Is(err, bufio.ErrBufferFull) {
				r.passLine = false
				if err != nil {
					r.err = err
				}
			}
			if len(frag) == 0 {
				return 0, r.err
			}
			n := copy(p, frag)
			r.pending = frag[n:]
			return n, nil
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			r.passLine = true
			n := copy(p, frag)
			r.pending = frag[n:]
			return n, nil
		}

		if len(frag) > 0 && isKeepAliveSSELine(frag) {
			if err != nil {
				r.err = err
				return 0, err
			}
			continue
		}

		if err != nil {
			r.err = err
		}
		if len(frag) == 0 {
			return 0, r.err
		}
		n := copy(p, frag)
		r.pending = frag[n:]
		return n, nil
	}
}
