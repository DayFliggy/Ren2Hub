package service

// PerformanceObservation is an instance-local cached view. History resets on
// process restart; missing samples and stale values are never filled with zero.
type PerformanceObservation struct {
	Scope             string               `json:"scope"`
	InstanceStartedAt int64                `json:"instance_started_at"`
	SampledAt         *int64               `json:"sampled_at"`
	Stale             bool                 `json:"stale"`
	Status            string               `json:"status"`
	Coverage          ObservationCoverage  `json:"coverage"`
	Checks            []ObservationCheck   `json:"checks"`
	Resources         ObservationResources `json:"resources"`
	Storage           ObservationStorage   `json:"storage"`
	Process           *ObservationProcess  `json:"process"`
	Traffic           ObservationTraffic   `json:"traffic"`
	Series            ObservationSeries    `json:"series"`
}

type ObservationCoverage struct {
	Measured int `json:"measured"`
	Total    int `json:"total"`
}

type ObservationCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type ObservationResources struct {
	CPU    ObservationResource `json:"cpu"`
	Memory ObservationResource `json:"memory"`
	Disk   ObservationResource `json:"disk"`
}

type ObservationResource struct {
	Available  bool     `json:"available"`
	Percent    *float64 `json:"percent"`
	Scope      string   `json:"scope"`
	SampledAt  *int64   `json:"sampled_at"`
	State      string   `json:"state"`
	UsedBytes  *uint64  `json:"used_bytes"`
	TotalBytes *uint64  `json:"total_bytes"`
	Threshold  int      `json:"threshold"`
}

type ObservationStorage struct {
	Database    ObservationProbe  `json:"database"`
	LogDatabase *ObservationProbe `json:"log_database"`
	LogShared   bool              `json:"log_shared"`
	Redis       ObservationProbe  `json:"redis"`
}

type ObservationProbe struct {
	Status    string   `json:"status"`
	LatencyMs *float64 `json:"latency_ms"`
	Reason    string   `json:"reason"`
}

type ObservationProcess struct {
	Goroutines     int      `json:"goroutines"`
	HeapAllocBytes uint64   `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64   `json:"heap_sys_bytes"`
	GCCount        uint32   `json:"gc_count"`
	LastGCPauseMs  *float64 `json:"last_gc_pause_ms"`
}

type ObservationTraffic struct {
	Scope        string   `json:"scope"`
	RequestMbps  *float64 `json:"request_mbps"`
	ResponseMbps *float64 `json:"response_mbps"`
}

type ObservationSeries struct {
	Window        string                   `json:"window"`
	BucketSeconds int64                    `json:"bucket_seconds"`
	Points        []ObservationSeriesPoint `json:"points"`
}

type ObservationSeriesPoint struct {
	Ts             int64    `json:"ts"`
	CPU            *float64 `json:"cpu"`
	Memory         *float64 `json:"memory"`
	Disk           *float64 `json:"disk"`
	RequestMbps    *float64 `json:"request_mbps"`
	ResponseMbps   *float64 `json:"response_mbps"`
	Goroutines     *float64 `json:"goroutines"`
	HeapAllocBytes *float64 `json:"heap_alloc_bytes"`
}
