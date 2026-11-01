package common

import (
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode"
)

const (
	TrafficScopeHostInterface       = "host_interface"
	TrafficScopeApplicationListener = "application_listener"
)

// A host-mode sample carries one of these reasons instead of rates.
const (
	TrafficReasonHostConfigInvalid      = "host_config_invalid"
	TrafficReasonHostCountersUnreadable = "host_counters_unreadable"
	TrafficReasonHostCountersInvalid    = "host_counters_invalid"
	TrafficReasonHostInterfaceMissing   = "host_interface_missing"
)

// defaultTrafficHostNetDev is where deployments mount the host's net/dev. It is
// a variable so tests can show that the file existing never selects host mode.
var defaultTrafficHostNetDev = "/host/proc/net/dev"

// TrafficSource is the startup decision. It does not change within a process,
// and a host-mode failure is reported through Reason, never by switching to
// the listener.
type TrafficSource struct {
	Scope     string
	Interface string
	NetDev    string
	Reason    string
}

var currentTrafficSource = sync.OnceValue(trafficSourceFromEnv)

// CurrentTrafficSource returns the traffic source decided on first use.
func CurrentTrafficSource() TrafficSource {
	return currentTrafficSource()
}

func trafficSourceFromEnv() TrafficSource {
	return selectTrafficSource(os.Getenv("TRAFFIC_HOST_INTERFACE"), os.Getenv("TRAFFIC_HOST_NET_DEV"))
}

// selectTrafficSource decides the mode from the raw values before applying the
// default path, so an empty value equals unset and only explicit configuration
// selects host mode.
func selectTrafficSource(iface, netDev string) TrafficSource {
	if iface == "" && netDev == "" {
		return TrafficSource{Scope: TrafficScopeApplicationListener}
	}
	if netDev == "" {
		netDev = defaultTrafficHostNetDev
	}
	source := TrafficSource{Scope: TrafficScopeHostInterface, Interface: iface, NetDev: netDev}
	// net/dev is a Linux file, so the path uses slash semantics on every
	// platform; elsewhere reading it reports host_counters_unreadable.
	if !validTrafficInterface(iface) || !path.IsAbs(netDev) {
		source.Reason = TrafficReasonHostConfigInvalid
	}
	return source
}

// validTrafficInterface follows the kernel's interface name limits: at most
// 15 bytes, without whitespace, '/', ':' or NUL.
func validTrafficInterface(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return r == '/' || r == ':' || r == 0 || unicode.IsSpace(r)
	})
}

// ReadHostTraffic returns the configured interface's cumulative rx and tx
// bytes. It rereads the whole file on every call. On failure it returns a
// reason and zero counters, never another interface's counters.
func ReadHostTraffic(source TrafficSource) (rx, tx uint64, reason string) {
	if source.Reason != "" {
		return 0, 0, source.Reason
	}
	content, reason := readNetDevFile(source.NetDev)
	if reason != "" {
		return 0, 0, reason
	}
	counters, ok := parseNetDev(content)
	if !ok {
		return 0, 0, TrafficReasonHostCountersInvalid
	}
	counter, ok := counters[source.Interface]
	if !ok {
		return 0, 0, TrafficReasonHostInterfaceMissing
	}
	return counter.rx, counter.tx, ""
}

// maxNetDevBytes bounds one read; a host with thousands of interfaces stays
// far below it. procfs reports size 0, so the bound is on bytes read.
const maxNetDevBytes = 1 << 20

// readNetDevFile reads the counter file without letting a wrong path stall
// the sampler: the open does not block (a FIFO without a writer), anything
// other than a regular file is refused (devices such as /dev/zero), and a
// file larger than maxNetDevBytes is invalid rather than read to the end.
func readNetDevFile(name string) ([]byte, string) {
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, TrafficReasonHostCountersUnreadable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, TrafficReasonHostCountersUnreadable
	}
	content, err := io.ReadAll(io.LimitReader(file, maxNetDevBytes+1))
	if err != nil {
		return nil, TrafficReasonHostCountersUnreadable
	}
	if len(content) > maxNetDevBytes {
		return nil, TrafficReasonHostCountersInvalid
	}
	return content, ""
}

type netDevCounter struct{ rx, tx uint64 }

// parseNetDev accepts only a whole, well-formed sample: two header lines
// containing '|', then one line per interface. A malformed or duplicate line
// rejects the sample, so no partial table is ever used.
func parseNetDev(content []byte) (map[string]netDevCounter, bool) {
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "|") || !strings.Contains(lines[1], "|") {
		return nil, false
	}
	counters := make(map[string]netDevCounter, len(lines)-2)
	for _, line := range lines[2:] {
		name, counter, ok := parseNetDevLine(line)
		if !ok {
			return nil, false
		}
		if _, duplicate := counters[name]; duplicate {
			return nil, false
		}
		counters[name] = counter
	}
	return counters, true
}

// parseNetDevLine splits at the first ':', so a counter glued to the colon is
// read. rx bytes is field 0 and tx bytes is field 8 of at least 16 decimal
// fields.
func parseNetDevLine(line string) (string, netDevCounter, bool) {
	name, rest, found := strings.Cut(line, ":")
	name = strings.TrimSpace(name)
	fields := strings.Fields(rest)
	if !found || name == "" || len(fields) < 16 {
		return "", netDevCounter{}, false
	}
	var counter netDevCounter
	for i, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return "", netDevCounter{}, false
		}
		switch i {
		case 0:
			counter.rx = value
		case 8:
			counter.tx = value
		}
	}
	return name, counter, true
}
