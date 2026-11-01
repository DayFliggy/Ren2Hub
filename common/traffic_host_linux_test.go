//go:build linux

package common

import (
	"io"
	"net"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A wrong TRAFFIC_HOST_NET_DEV must only make traffic unavailable: it may not
// block the sampler, which also publishes CPU, memory and storage, or grow
// memory without bound.
func TestHostTrafficRefusesSpecialFilesWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "net-dev.fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	for _, path := range []string{fifo, "/dev/zero", "/dev/null"} {
		done := make(chan string, 1)
		go func() {
			_, _, reason := ReadHostTraffic(hostSource("eth0", path))
			done <- reason
		}()
		select {
		case reason := <-done:
			require.Equal(t, TrafficReasonHostCountersUnreadable, reason, path)
		case <-time.After(5 * time.Second):
			t.Fatalf("reading %s blocked the sampler", path)
		}
	}
}

func TestHostTrafficReadsProcfsCounters(t *testing.T) {
	// procfs stats as a 0-byte regular file; the real file must still be read,
	// and loopback traffic sent here must show up in its counters.
	source := hostSource("lo", "/proc/self/net/dev")
	rxBefore, txBefore, reason := ReadHostTraffic(source)
	require.Empty(t, reason)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	payload := make([]byte, 64<<10)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_, _ = io.Copy(io.Discard, conn)
			conn.Close()
		}
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = conn.Write(payload)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		rx, tx, reason := ReadHostTraffic(source)
		return reason == "" && rx >= rxBefore+uint64(len(payload)) && tx >= txBefore+uint64(len(payload))
	}, 5*time.Second, 50*time.Millisecond)
}
