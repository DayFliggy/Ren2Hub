package common

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
)

// TrafficCounter holds the bytes read from and written to the connections a
// counting listener accepted, seen from the server: in is Read, out is Write.
type TrafficCounter struct {
	in, out atomic.Uint64
}

// listenerTraffic counts the main HTTP listener; main wraps it with
// CountingListener and the performance observer reads ListenerTrafficSnapshot.
var listenerTraffic TrafficCounter

// CountingListener wraps the main HTTP listener so every accepted connection
// reports its bytes, including headers, bodies and hijacked WebSocket frames.
func CountingListener(ln net.Listener) net.Listener {
	return listenerTraffic.Listener(ln)
}

// ListenerTrafficSnapshot returns the cumulative bytes of the main listener.
func ListenerTrafficSnapshot() (in, out uint64) {
	return listenerTraffic.Snapshot()
}

func (c *TrafficCounter) Listener(ln net.Listener) net.Listener {
	return &countingListener{Listener: ln, counter: c}
}

func (c *TrafficCounter) Snapshot() (in, out uint64) {
	return c.in.Load(), c.out.Load()
}

type countingListener struct {
	net.Listener
	counter *TrafficCounter
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, counter: l.counter}, nil
}

// countingConn embeds the net.Conn interface value, not *net.TCPConn, so
// ReadFrom, WriteTo and SyscallConn are not promoted and no copy path can
// bypass Read and Write.
type countingConn struct {
	net.Conn
	counter *TrafficCounter
}

// Read counts the bytes it returns even when an error comes with them.
func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.counter.in.Add(uint64(n))
	}
	return n, err
}

// Write counts the bytes it returns even when an error comes with them.
func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.counter.out.Add(uint64(n))
	}
	return n, err
}

// CloseWrite keeps the half-close net/http sends before closing a connection.
func (c *countingConn) CloseWrite() error {
	closer, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return fmt.Errorf("close write on %T: %w", c.Conn, errors.ErrUnsupported)
	}
	return closer.CloseWrite()
}
