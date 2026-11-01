package common

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const netDevHeader = "Inter-|   Receive                                                |  Transmit\n" +
	" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n"

func netDevRow(name, rx, tx string) string {
	return fmt.Sprintf("%6s: %s 2 0 0 0 0 0 0 %s 4 0 0 0 0 0 0\n", name, rx, tx)
}

func writeNetDev(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "dev")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	return file
}

func hostSource(iface, netDev string) TrafficSource {
	return TrafficSource{Scope: TrafficScopeHostInterface, Interface: iface, NetDev: netDev}
}

func TestParseNetDevAcceptsCounterGluedToColonAndLargeValues(t *testing.T) {
	content := netDevHeader +
		"    lo: 1234567     100    0    0    0     0          0         0  7654321     100    0    0    0     0       0          0\n" +
		"  eth0:9007199254740993 2 0 0 0 0 0 0 18446744073709551615 4 0 0 0 0 0 0\n"
	counters, ok := parseNetDev([]byte(content))
	require.True(t, ok)
	require.Equal(t, map[string]netDevCounter{
		"lo":   {rx: 1234567, tx: 7654321},
		"eth0": {rx: 9007199254740993, tx: math.MaxUint64},
	}, counters, "a counter glued to the colon is read, and values above 2^53 stay exact")

	rx, tx, reason := ReadHostTraffic(hostSource("eth0", writeNetDev(t, content)))
	require.Empty(t, reason)
	require.Equal(t, uint64(9007199254740993), rx)
	require.Equal(t, uint64(math.MaxUint64), tx)
}

func TestParseNetDevRejectsMalformedSample(t *testing.T) {
	eth0 := netDevRow("eth0", "100", "200")
	for _, tc := range []struct{ name, content string }{
		{"empty file", ""},
		{"missing header", eth0 + netDevRow("eth1", "1", "1")},
		{"short line", netDevHeader + eth0 + "  eth1: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15\n"},
		{"non-decimal", netDevHeader + eth0 + netDevRow("eth1", "0x10", "1")},
		{"non-decimal unused field", netDevHeader + eth0 + "  eth1: 1 2 3 4 5 6 7 x 9 10 11 12 13 14 15 16\n"},
		{"negative", netDevHeader + eth0 + netDevRow("eth1", "1", "-1")},
		{"overflow", netDevHeader + eth0 + netDevRow("eth1", "18446744073709551616", "1")},
		{"duplicate interface", netDevHeader + eth0 + netDevRow("eth0", "300", "400")},
		{"missing colon", netDevHeader + eth0 + "  eth1 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\n"},
		{"empty interface name", netDevHeader + eth0 + netDevRow("", "1", "1")},
		{"blank line", netDevHeader + eth0 + "\n" + netDevRow("eth1", "1", "1")},
	} {
		counters, ok := parseNetDev([]byte(tc.content))
		require.False(t, ok, tc.name)
		require.Nil(t, counters, tc.name)
		// The configured eth0 line is well formed, yet the whole sample is rejected.
		rx, tx, reason := ReadHostTraffic(hostSource("eth0", writeNetDev(t, tc.content)))
		require.Equal(t, TrafficReasonHostCountersInvalid, reason, tc.name)
		require.Zero(t, rx, tc.name)
		require.Zero(t, tx, tc.name)
	}
}

func TestHostTrafficRejectsOversizedCounterFile(t *testing.T) {
	// Every row is well formed and eth0 comes first, so only the size bound can reject it.
	var content strings.Builder
	content.WriteString(netDevHeader + netDevRow("eth0", "100", "200"))
	for i := 0; content.Len() <= maxNetDevBytes; i++ {
		content.WriteString(netDevRow(fmt.Sprintf("if%d", i), "1", "1"))
	}
	rx, tx, reason := ReadHostTraffic(hostSource("eth0", writeNetDev(t, content.String())))
	require.Equal(t, TrafficReasonHostCountersInvalid, reason)
	require.Zero(t, rx)
	require.Zero(t, tx)

	// A huge regular file is not read to the end: a 64 MiB sparse file costs about the bound, not its size.
	huge := filepath.Join(t.TempDir(), "huge")
	require.NoError(t, os.WriteFile(huge, []byte(netDevHeader), 0o644))
	require.NoError(t, os.Truncate(huge, 64<<20))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _, reason = ReadHostTraffic(hostSource("eth0", huge))
	runtime.ReadMemStats(&after)
	require.Equal(t, TrafficReasonHostCountersInvalid, reason)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20))

	// Exactly at the bound is still read: pad the gap between two fields of a valid row.
	row := netDevRow("eth0", "100", "200")
	padding := maxNetDevBytes - len(netDevHeader) - len(row)
	limit := netDevHeader + strings.Replace(row, " 2 ", " "+strings.Repeat(" ", padding)+"2 ", 1)
	require.Len(t, limit, maxNetDevBytes)
	rx, tx, reason = ReadHostTraffic(hostSource("eth0", writeNetDev(t, limit)))
	require.Empty(t, reason)
	require.Equal(t, [2]uint64{100, 200}, [2]uint64{rx, tx})
}

func TestHostTrafficReportsReasonWithoutReading(t *testing.T) {
	// Every case could read some other interface; none may.
	others := netDevHeader + netDevRow("eth1", "111", "222") + netDevRow("eth00", "333", "444")
	directory := t.TempDir()
	cases := []struct {
		name   string
		source TrafficSource
		reason string
	}{
		{"missing file", hostSource("eth1", filepath.Join(directory, "absent")), TrafficReasonHostCountersUnreadable},
		{"read failure", hostSource("eth1", directory), TrafficReasonHostCountersUnreadable},
		{"interface missing, no prefix or fallback match", hostSource("eth0", writeNetDev(t, others)), TrafficReasonHostInterfaceMissing},
		{"invalid config is not read", selectTrafficSource("eth1 ", writeNetDev(t, others)), TrafficReasonHostConfigInvalid},
	}
	if os.Geteuid() != 0 {
		denied := writeNetDev(t, others)
		require.NoError(t, os.Chmod(denied, 0))
		cases = append(cases, struct {
			name   string
			source TrafficSource
			reason string
		}{"permission denied", hostSource("eth1", denied), TrafficReasonHostCountersUnreadable})
	}
	for _, tc := range cases {
		rx, tx, reason := ReadHostTraffic(tc.source)
		require.Equal(t, tc.reason, reason, tc.name)
		require.Zero(t, rx, tc.name)
		require.Zero(t, tx, tc.name)
	}
}

func TestTrafficSourceSelection(t *testing.T) {
	previous := defaultTrafficHostNetDev
	t.Cleanup(func() { defaultTrafficHostNetDev = previous })
	defaultTrafficHostNetDev = writeNetDev(t, netDevHeader+netDevRow("eth0", "1", "2"))
	listener := TrafficSource{Scope: TrafficScopeApplicationListener}
	keys := []string{"TRAFFIC_HOST_INTERFACE", "TRAFFIC_HOST_NET_DEV"}
	for _, tc := range []struct {
		name string
		env  map[string]string // a missing key is unset
		want TrafficSource
	}{
		{"both unset uses the listener even though the default file exists", nil, listener},
		{"empty strings equal unset", map[string]string{"TRAFFIC_HOST_INTERFACE": "", "TRAFFIC_HOST_NET_DEV": ""}, listener},
		{"interface only reads the default path", map[string]string{"TRAFFIC_HOST_INTERFACE": "eth0"}, hostSource("eth0", defaultTrafficHostNetDev)},
		{"an empty path equals unset", map[string]string{"TRAFFIC_HOST_INTERFACE": "eth0", "TRAFFIC_HOST_NET_DEV": ""}, hostSource("eth0", defaultTrafficHostNetDev)},
		{"path only is host mode with an invalid config", map[string]string{"TRAFFIC_HOST_NET_DEV": "/proc/net/dev"},
			TrafficSource{Scope: TrafficScopeHostInterface, NetDev: "/proc/net/dev", Reason: TrafficReasonHostConfigInvalid}},
		{"an empty interface equals unset", map[string]string{"TRAFFIC_HOST_INTERFACE": "", "TRAFFIC_HOST_NET_DEV": "/proc/net/dev"},
			TrafficSource{Scope: TrafficScopeHostInterface, NetDev: "/proc/net/dev", Reason: TrafficReasonHostConfigInvalid}},
		{"both set", map[string]string{"TRAFFIC_HOST_INTERFACE": "ens5", "TRAFFIC_HOST_NET_DEV": "/proc/net/dev"}, hostSource("ens5", "/proc/net/dev")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range keys {
				t.Setenv(key, "")
				require.NoError(t, os.Unsetenv(key))
				if value, ok := tc.env[key]; ok {
					t.Setenv(key, value)
				}
			}
			require.Equal(t, tc.want, trafficSourceFromEnv())
		})
	}

	rx, tx, reason := ReadHostTraffic(selectTrafficSource("eth0", ""))
	require.Equal(t, [2]uint64{1, 2}, [2]uint64{rx, tx}, "the default path is the file that is read")
	require.Empty(t, reason)

	for name, valid := range map[string]bool{
		"eth0": true, "a23456789012345": true, "a234567890123456": false,
		" eth0": false, "eth 0": false, "eth\t0": false, "eth/0": false, "eth:0": false, "eth\x000": false,
	} {
		got := selectTrafficSource(name, "/proc/net/dev")
		require.Equal(t, TrafficScopeHostInterface, got.Scope, "%q: an invalid config never falls back to the listener", name)
		require.Equal(t, name, got.Interface)
		require.Equal(t, valid, got.Reason == "", "%q", name)
	}
	require.Equal(t, TrafficReasonHostConfigInvalid, selectTrafficSource("eth0", "proc/net/dev").Reason, "a relative path is invalid")
}
