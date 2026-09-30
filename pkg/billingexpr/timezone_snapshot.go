package billingexpr

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// A returned time.Time can be used with AddDate/In/Format later in the same
// expression. Preserve the timezone rules, not just its offset at one instant.
func snapshotLocation(location *time.Location) ([]byte, error) {
	name := location.String()
	if location == time.UTC {
		return nil, nil
	}
	if name == "Local" && runtime.GOOS != "windows" {
		zone, exists := os.LookupEnv("TZ")
		zone = strings.TrimPrefix(zone, ":")
		if !exists {
			zone = "/etc/localtime"
		}
		if filepath.IsAbs(zone) {
			if data, err := os.ReadFile(zone); err == nil {
				return data, nil
			}
		}
		if zone != "" {
			name = zone
		}
	}
	if filepath.IsAbs(name) {
		if data, err := os.ReadFile(name); err == nil {
			return data, nil
		}
	}
	sources := []string{os.Getenv("ZONEINFO"), "/usr/share/zoneinfo", "/usr/share/lib/zoneinfo", "/usr/lib/locale/TZ", "/etc/zoneinfo", filepath.Join(runtime.GOROOT(), "lib", "time", "zoneinfo.zip")}
	if !strings.Contains(name, "..") && !filepath.IsAbs(name) {
		for _, source := range sources {
			if source == "" {
				continue
			}
			if strings.HasSuffix(source, ".zip") {
				archive, err := zip.OpenReader(source)
				if err != nil {
					continue
				}
				var data []byte
				for _, file := range archive.File {
					if file.Name != name {
						continue
					}
					reader, err := file.Open()
					if err != nil {
						break
					}
					data, err = io.ReadAll(io.LimitReader(reader, 16<<20))
					reader.Close()
					if err != nil {
						data = nil
					}
					break
				}
				archive.Close()
				if data != nil {
					return data, nil
				}
			} else if data, err := os.ReadFile(filepath.Join(source, name)); err == nil {
				return data, nil
			}
		}
	}
	// Go's Windows Local location has a finite transition table. Fixed zones
	// (including offsets parsed by date()) have no transitions at all.
	return snapshotFiniteLocation(location)
}

func snapshotFiniteLocation(location *time.Location) ([]byte, error) {
	type zone struct {
		name   string
		offset int
		dst    bool
	}
	var zones []zone
	var transitions []int64
	var indexes []byte
	at := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).In(location)
	for step := 0; step < 1024; step++ {
		name, offset := at.Zone()
		entry := zone{name, offset, at.IsDST()}
		index := -1
		for i, known := range zones {
			if entry == known {
				index = i
				break
			}
		}
		if index < 0 {
			if len(zones) == 256 {
				return nil, fmt.Errorf("billing timezone has too many states")
			}
			index = len(zones)
			zones = append(zones, entry)
		}
		if step > 0 {
			transitions = append(transitions, at.Unix())
			indexes = append(indexes, byte(index))
		}
		_, end := at.ZoneBounds()
		if end.IsZero() {
			var output bytes.Buffer
			writeHeader := func(count, zoneCount, chars int) {
				output.WriteString("TZif2")
				output.Write(make([]byte, 15))
				for _, n := range []int{0, 0, 0, count, zoneCount, chars} {
					binary.Write(&output, binary.BigEndian, uint32(n))
				}
			}
			writeHeader(0, 1, 4)
			output.Write([]byte{0, 0, 0, 0, 0, 0})
			output.WriteString("UTC\x00")
			var abbreviations bytes.Buffer
			names := make([]byte, len(zones))
			for i, z := range zones {
				if abbreviations.Len() > 255 {
					return nil, fmt.Errorf("billing timezone names exceed limit")
				}
				names[i] = byte(abbreviations.Len())
				abbreviations.WriteString(z.name)
				abbreviations.WriteByte(0)
			}
			writeHeader(len(transitions), len(zones), abbreviations.Len())
			for _, when := range transitions {
				binary.Write(&output, binary.BigEndian, when)
			}
			output.Write(indexes)
			for i, z := range zones {
				binary.Write(&output, binary.BigEndian, int32(z.offset))
				isDST := byte(0)
				if z.dst {
					isDST = 1
				}
				output.WriteByte(isDST)
				output.WriteByte(names[i])
			}
			output.Write(abbreviations.Bytes())
			output.WriteString("\n\n")
			return output.Bytes(), nil
		}
		if !end.After(at) {
			return nil, fmt.Errorf("billing timezone boundaries are invalid")
		}
		at = end.In(location)
	}
	return nil, fmt.Errorf("billing timezone source cannot be frozen")
}
