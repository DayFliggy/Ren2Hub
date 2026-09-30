package billingexpr

import (
	"fmt"
	"reflect"
	"runtime"
	"time"
	"unsafe"
)

// Go intentionally omits monotonic readings from its public time encodings.
// Persist both integer fields so Sub/Before/Equal/String keep their first-run
// semantics. The original Location pointer is never serialized.
type timeState struct {
	Toolchain string `json:"toolchain"`
	Wall      uint64 `json:"wall,string"`
	Ext       int64  `json:"ext,string"`
}

type timeLayout struct {
	wall uint64
	ext  int64
	loc  *time.Location
}

func validateTimeLayout(actual reflect.Type) error {
	want := reflect.TypeOf(timeLayout{})
	if actual.Kind() != reflect.Struct || actual.NumField() != want.NumField() || actual.Size() != want.Size() || actual.Align() != want.Align() {
		return fmt.Errorf("unsupported Go time layout")
	}
	for i := range want.NumField() {
		a, b := actual.Field(i), want.Field(i)
		if a.Name != b.Name || a.Type != b.Type || a.Offset != b.Offset || a.Type.Size() != b.Type.Size() {
			return fmt.Errorf("unsupported Go time layout")
		}
	}
	return nil
}

func captureTimeState(at time.Time) (*timeState, error) {
	if err := validateTimeLayout(reflect.TypeOf(at)); err != nil {
		return nil, err
	}
	raw := *(*timeLayout)(unsafe.Pointer(&at))
	return &timeState{Toolchain: runtime.Version(), Wall: raw.wall, Ext: raw.ext}, nil
}

func restoreTimeState(state *timeState, location *time.Location) (time.Time, error) {
	if err := validateTimeLayout(reflect.TypeOf(time.Time{})); err != nil {
		return time.Time{}, err
	}
	if state == nil || state.Toolchain != runtime.Version() || location == nil {
		return time.Time{}, fmt.Errorf("recorded Go time toolchain is unavailable")
	}
	if state.Wall&((1<<30)-1) >= 1000000000 || state.Wall&(1<<63) == 0 && state.Wall>>30 != 0 {
		return time.Time{}, fmt.Errorf("invalid recorded Go time state")
	}
	if location == time.UTC {
		location = nil
	}
	raw := timeLayout{wall: state.Wall, ext: state.Ext, loc: location}
	return *(*time.Time)(unsafe.Pointer(&raw)), nil
}
