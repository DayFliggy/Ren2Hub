package service

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ParseViewerLocation is the single parser for the tz_offset query parameter.
// The value follows the browser's Date.getTimezoneOffset() convention (minutes,
// positive west of UTC); an empty value means UTC. The fixed offset does not
// follow daylight-saving changes inside a queried window.
func ParseViewerLocation(raw string) (*time.Location, error) {
	if raw == "" {
		return time.UTC, nil
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < -14*60 || offset > 14*60 {
		return nil, common.NewMessage("Invalid timezone offset")
	}
	return time.FixedZone("viewer", -offset*60), nil
}

// viewerOffset converts t's zone back to the tz_offset convention.
func viewerOffset(t time.Time) int {
	_, seconds := t.Zone()
	return -seconds / 60
}
