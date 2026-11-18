package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// The request policy lock protects the latest selected scan until dispatch or rejection.
// A rejected scan already has its own server log.
type routeScanState struct {
	round     int
	pending   *routeScan
	channelID int
}

func (s *routeScanState) track(event PolicyEvent) {
	s.round++
	event.Scan.Round = s.round
	s.pending = nil
	if event.ChannelID != 0 {
		s.pending = event.Scan
		s.channelID = event.ChannelID
	}
}

func (s *routeScan) message(result string) string {
	return fmt.Sprintf("route_scan round=%d candidates=%d full_skips=%d result=%s duration_us=%d",
		s.Round, s.Candidates, s.FullSkips, result, s.DurationUS)
}

func takeUnsentRouteScan(c *gin.Context) (*routeScan, int) {
	v, ok := c.Get(requestPolicyContextKey)
	if !ok {
		return nil, 0
	}
	state := v.(*RequestPolicyState)
	state.mu.Lock()
	defer state.mu.Unlock()
	scan := state.scans.pending
	state.scans.pending = nil
	return scan, state.scans.channelID
}

// MarkRouteScanDispatched consumes the pending selection before an upstream send.
func MarkRouteScanDispatched(c *gin.Context) {
	takeUnsentRouteScan(c)
}

// LogRejectedRouteSelection makes a local rejection traceable even without a billing log.
func LogRejectedRouteSelection(c *gin.Context, status int) {
	if scan, id := takeUnsentRouteScan(c); scan != nil {
		logger.LogWarn(c, "%s channel_id=%d request_status=%d", scan.message("selected"), id, status)
	}
}

// RecordTerminalRelayErrorLog includes the final decision in a local rejection's error row.
func RecordTerminalRelayErrorLog(c *gin.Context, channelID int, apiErr *types.NewAPIError, info *relaycommon.RelayInfo) {
	RecordRequestPolicyTermination(c, apiErr)
	RecordRelayErrorLog(c, channelID, apiErr, info)
}
