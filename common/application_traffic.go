package common

import "sync/atomic"

// These counters measure HTTP body bytes read from clients and written back to
// clients. They exclude headers, network overhead and hijacked WebSocket frames.
var applicationRequestBytes atomic.Uint64
var applicationResponseBytes atomic.Uint64

func ObserveApplicationRequestBytes(n int) {
	if n > 0 {
		applicationRequestBytes.Add(uint64(n))
	}
}

func ObserveApplicationResponseBytes(n int) {
	if n > 0 {
		applicationResponseBytes.Add(uint64(n))
	}
}

func ApplicationTrafficSnapshot() (requestBytes, responseBytes uint64) {
	return applicationRequestBytes.Load(), applicationResponseBytes.Load()
}
