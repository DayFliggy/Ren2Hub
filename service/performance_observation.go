package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	observationInterval     = 10 * time.Second
	observationRetention    = 24 * time.Hour
	observationMaxAge       = 30 * time.Second
	observationProbeTimeout = 2 * time.Second
)

var ErrObservationWindow = errors.New("invalid observation window")

var observationWindows = map[string]struct{ span, bucket time.Duration }{
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, 5 * time.Minute},
	"24h": {24 * time.Hour, 15 * time.Minute},
}

type observationSampler struct {
	mu          sync.RWMutex
	latest      *PerformanceObservation
	ring        []ObservationSeriesPoint
	next, count int
	// traffic, trafficReason and failing belong to the single sampling loop.
	traffic       trafficSampler
	trafficReason string
	failing       map[string]bool
}

func newObservationSampler(capacity int) *observationSampler {
	return &observationSampler{ring: make([]ObservationSeriesPoint, capacity), failing: make(map[string]bool)}
}

var performanceObserver = newObservationSampler(int(observationRetention/observationInterval) + 1)
var performanceObserverOnce sync.Once
var stopPerformanceObserver func()

// StartPerformanceObserver owns the only probe/sampling loop. Its stop function
// cancels outstanding probes and waits for completion before database shutdown.
func StartPerformanceObserver() func() {
	performanceObserverOnce.Do(func() {
		logTrafficSource(common.CurrentTrafficSource())
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			performanceObserver.sample(ctx)
			ticker := time.NewTicker(observationInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					performanceObserver.sample(ctx)
				}
			}
		}()
		stopPerformanceObserver = func() { cancel(); <-done }
	})
	return stopPerformanceObserver
}

// ReadPerformanceObservation never probes dependencies. Dashboard and ops read
// the same cached sample, with a caller-selected projection of retained history.
func ReadPerformanceObservation(window string) (PerformanceObservation, error) {
	return performanceObserver.read(window, time.Now())
}

func (s *observationSampler) sample(ctx context.Context) {
	started := time.Now()
	monitor := common.GetPerformanceMonitorConfig()
	status := common.GetSystemStatus()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	observation := emptyPerformanceObservation()
	observation.SampledAt = observationPtr(started.Unix())
	observation.Traffic = s.sampleTraffic(common.CurrentTrafficSource(), started)
	observation.Resources = observationResources(status, monitor, started)
	observation.Process = observationProcess(mem, runtime.NumGoroutine())
	observation.Storage = s.probeStorage(ctx)
	if ctx.Err() == nil {
		s.record(observation)
	}
}

func observationProcess(mem runtime.MemStats, goroutines int) *ObservationProcess {
	result := &ObservationProcess{
		Goroutines: goroutines, HeapAllocBytes: mem.HeapAlloc,
		HeapSysBytes: mem.HeapSys, GCCount: mem.NumGC,
	}
	if mem.NumGC > 0 {
		result.LastGCPauseMs = observationPtr(float64(mem.PauseNs[(mem.NumGC-1)%uint32(len(mem.PauseNs))]) / 1e6)
	}
	return result
}

func emptyPerformanceObservation() PerformanceObservation {
	resource := ObservationResource{Scope: "unknown", State: "unknown"}
	probe := ObservationProbe{Status: "unknown", Reason: "not_sampled"}
	return PerformanceObservation{
		Scope: "instance", InstanceStartedAt: common.StartTime, Status: "unknown",
		Resources: ObservationResources{CPU: resource, Memory: resource, Disk: resource},
		Storage:   ObservationStorage{Database: probe, Redis: probe},
		Traffic:   trafficView(common.CurrentTrafficSource()),
	}
}

// trafficView carries the startup decision without rates; an invalid host
// configuration is visible before the first sample.
func trafficView(source common.TrafficSource) ObservationTraffic {
	return ObservationTraffic{Scope: source.Scope, Interface: source.Interface, Reason: source.Reason}
}

func logTrafficSource(source common.TrafficSource) {
	if source.Scope == common.TrafficScopeApplicationListener {
		common.SysLog("performance observation: no host interface configured, counting the main listener")
		return
	}
	common.SysLog(fmt.Sprintf("performance observation: traffic from host interface %q in %s", source.Interface, source.NetDev))
}

// sampleTraffic reads the startup source and logs only reason transitions:
// entering, changing and recovering, not every interval.
func (s *observationSampler) sampleTraffic(source common.TrafficSource, now time.Time) ObservationTraffic {
	var in, out uint64
	var reason string
	if source.Scope == common.TrafficScopeApplicationListener {
		in, out = common.ListenerTrafficSnapshot()
	} else {
		in, out, reason = common.ReadHostTraffic(source)
	}
	if reason != s.trafficReason {
		if reason == "" {
			common.SysLog(fmt.Sprintf("performance observation: host traffic recovered (interface %q)", source.Interface))
		} else {
			common.SysError(fmt.Sprintf("performance observation: host traffic unavailable (%s, interface %q, file %s)", reason, source.Interface, source.NetDev))
		}
		s.trafficReason = reason
	}
	traffic := trafficView(source)
	traffic.Reason = reason
	traffic.InboundMbps, traffic.OutboundMbps = s.traffic.sample(now, in, out, reason)
	return traffic
}

func observationResources(status common.SystemStatus, monitor common.PerformanceMonitorConfig, now time.Time) ObservationResources {
	cpu := makeObservationResource(status, monitor.Enabled, status.CPUAvailable, status.CPUUsage, status.CPUScope, monitor.CPUThreshold, now)
	memory := makeObservationResource(status, monitor.Enabled, status.MemoryAvailable, status.MemoryUsage, status.MemoryScope, monitor.MemoryThreshold, now)
	disk := makeObservationResource(status, monitor.Enabled, status.DiskAvailable, status.DiskUsage, "filesystem", monitor.DiskThreshold, now)
	if memory.Available {
		memory.UsedBytes, memory.TotalBytes = observationPtr(status.MemoryUsedBytes), observationPtr(status.MemoryTotalBytes)
	}
	if disk.Available {
		disk.UsedBytes, disk.TotalBytes = observationPtr(status.DiskUsedBytes), observationPtr(status.DiskTotalBytes)
	}
	return ObservationResources{CPU: cpu, Memory: memory, Disk: disk}
}

func makeObservationResource(status common.SystemStatus, enabled, available bool, value float64, scope string, threshold int, now time.Time) ObservationResource {
	resource := ObservationResource{Scope: scope, State: "unknown", Threshold: threshold}
	if scope == "" {
		resource.Scope = "unknown"
	}
	if !status.SampledAt.IsZero() {
		resource.SampledAt = observationPtr(status.SampledAt.Unix())
	}
	if !enabled {
		resource.State = "disabled"
		return resource
	}
	if !status.SampledAt.IsZero() && !common.SystemStatusFresh(status, now) {
		resource.State = "stale"
		return resource
	}
	if available && common.SystemStatusFresh(status, now) {
		resource.Available, resource.State, resource.Percent = true, "ok", observationPtr(value)
	}
	return resource
}

func (s *observationSampler) record(observation PerformanceObservation) {
	point := observationPoint(observation)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = &observation
	s.ring[s.next] = point
	s.next = (s.next + 1) % len(s.ring)
	s.count = min(s.count+1, len(s.ring))
}

func observationPoint(observation PerformanceObservation) ObservationSeriesPoint {
	point := ObservationSeriesPoint{
		Ts: *observation.SampledAt, CPU: observation.Resources.CPU.Percent,
		Memory: observation.Resources.Memory.Percent, Disk: observation.Resources.Disk.Percent,
		InboundMbps: observation.Traffic.InboundMbps, OutboundMbps: observation.Traffic.OutboundMbps,
	}
	if observation.Process != nil {
		point.Goroutines = observationPtr(float64(observation.Process.Goroutines))
		point.HeapAllocBytes = observationPtr(float64(observation.Process.HeapAllocBytes))
	}
	return point
}

func (s *observationSampler) read(window string, now time.Time) (PerformanceObservation, error) {
	spec, ok := observationWindows[window]
	if !ok {
		return PerformanceObservation{}, ErrObservationWindow
	}
	observation := emptyPerformanceObservation()
	s.mu.RLock()
	if s.latest != nil {
		observation = *s.latest
	}
	points := make([]ObservationSeriesPoint, 0, s.count)
	since := now.Add(-spec.span).Unix()
	for i := 0; i < s.count; i++ {
		point := s.ring[(s.next-s.count+i+len(s.ring))%len(s.ring)]
		if point.Ts >= since && point.Ts <= now.Unix() {
			points = append(points, point)
		}
	}
	s.mu.RUnlock()
	if observation.SampledAt != nil && (now.Unix() < *observation.SampledAt || now.Unix()-*observation.SampledAt > int64(observationMaxAge.Seconds())) {
		observation.Stale = true
		invalidateObservation(&observation)
	}
	// Threshold changes apply to cached measurements immediately, like the
	// overload guard; an enabled unsampled check must remain unknown.
	monitor := common.GetPerformanceMonitorConfig()
	observation.Resources.CPU.Threshold = monitor.CPUThreshold
	observation.Resources.Memory.Threshold = monitor.MemoryThreshold
	observation.Resources.Disk.Threshold = monitor.DiskThreshold
	for _, resource := range []*ObservationResource{&observation.Resources.CPU, &observation.Resources.Memory, &observation.Resources.Disk} {
		if resource.State == "ok" && resource.SampledAt != nil && !common.SystemStatusFresh(common.SystemStatus{SampledAt: time.Unix(*resource.SampledAt, 0)}, now) {
			invalidateResource(resource, "stale")
		}
	}
	// A hot configuration change takes effect immediately, even before sampling.
	if !monitor.Enabled {
		invalidateResource(&observation.Resources.CPU, "disabled")
		invalidateResource(&observation.Resources.Memory, "disabled")
		invalidateResource(&observation.Resources.Disk, "disabled")
	}
	observation.Checks, observation.Coverage, observation.Status = observationHealth(observation)
	observation.Series = buildObservationSeries(points, window, since, now.Unix(), int64(spec.bucket.Seconds()))
	return observation, nil
}

func invalidateResource(resource *ObservationResource, state string) {
	resource.Available, resource.State, resource.Percent = false, state, nil
	resource.UsedBytes, resource.TotalBytes = nil, nil
}

func invalidateObservation(observation *PerformanceObservation) {
	invalidateResource(&observation.Resources.CPU, "stale")
	invalidateResource(&observation.Resources.Memory, "stale")
	invalidateResource(&observation.Resources.Disk, "stale")
	staleProbe := func(probe ObservationProbe) ObservationProbe {
		if probe.Status != "disabled" {
			probe.Status, probe.Reason, probe.LatencyMs = "stale", "stale", nil
		}
		return probe
	}
	observation.Storage.Database = staleProbe(observation.Storage.Database)
	observation.Storage.Redis = staleProbe(observation.Storage.Redis)
	if observation.Storage.LogDatabase != nil {
		probe := staleProbe(*observation.Storage.LogDatabase)
		observation.Storage.LogDatabase = &probe
	}
	observation.Process = nil
	observation.Traffic.InboundMbps, observation.Traffic.OutboundMbps = nil, nil
}

func observationPtr[T any](value T) *T { return &value }
