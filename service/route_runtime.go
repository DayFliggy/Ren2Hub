package service

import "errors"

var (
	ErrGatewayStorageUnavailable = errors.New("gateway storage unavailable")
	ErrAdmissionCapacity         = errors.New("gateway admission capacity exhausted")
	ErrAdmissionRuntime          = errors.New("gateway admission configuration changed or unavailable")
)

const (
	AdmissionFailureCode        = "route_admission_failed"
	AdmissionStateAcquired      = "acquired"
	AdmissionStateReleased      = "released"
	AdmissionStateRenewalFailed = "maintenance_failed"
)

type RouteRuntimeState struct {
	ChannelEnabled    bool
	HealthEpoch       int64
	CapabilityVersion int64
	Capacity          int
	ChannelRatio      float64
}

func RecheckRouteRuntime(expected, current RouteRuntimeState) error {
	// The stable admission manager owns capacity changes and already granted
	// this permit under its current policy. Capacity is not a qualification
	// fence: rejecting a woken waiter would defeat expansion during its wait.
	if !expected.ChannelEnabled || !current.ChannelEnabled ||
		expected.Capacity <= 0 || current.Capacity <= 0 ||
		expected.ChannelRatio <= 0 || expected.ChannelRatio != current.ChannelRatio ||
		expected.HealthEpoch != current.HealthEpoch ||
		expected.CapabilityVersion != current.CapabilityVersion {
		return ErrAdmissionRuntime
	}
	return nil
}
