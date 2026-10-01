package service

// OpsStatus grades one health check or the whole instance. Unknown means the
// signal is not measured; it never counts as healthy or unhealthy.
type OpsStatus string

const (
	OpsStatusOK       OpsStatus = "ok"
	OpsStatusWarning  OpsStatus = "warning"
	OpsStatusCritical OpsStatus = "critical"
	OpsStatusUnknown  OpsStatus = "unknown"
)

// OpsCheck is one graded signal. Value carries the measured number when the
// check has one: a percentage, a per-minute rate or a latency in milliseconds.
// Threshold 0 means the limit is disabled.
type OpsCheck struct {
	Key       string    `json:"key"`
	Status    OpsStatus `json:"status"`
	Value     *float64  `json:"value,omitempty"`
	Threshold *int      `json:"threshold,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type OpsRuntime struct {
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	NodeName      string `json:"node_name"`
	StartedAt     int64  `json:"started_at"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Master        bool   `json:"master"`
	Draining      bool   `json:"draining"`
}

type OpsPercent struct {
	Percent   *float64 `json:"percent"`
	Threshold int      `json:"threshold"`
}

type OpsUsage struct {
	Percent    *float64 `json:"percent"`
	UsedBytes  *uint64  `json:"used_bytes"`
	TotalBytes *uint64  `json:"total_bytes"`
	Threshold  int      `json:"threshold"`
	Path       string   `json:"path,omitempty"`
}

type OpsNetwork struct {
	UpMbps   *float64 `json:"up_mbps"`
	DownMbps *float64 `json:"down_mbps"`
}

type OpsProcess struct {
	Goroutines     int     `json:"goroutines"`
	HeapAllocBytes uint64  `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64  `json:"heap_sys_bytes"`
	GCCount        uint32  `json:"gc_count"`
	LastGCPauseMs  float64 `json:"last_gc_pause_ms"`
}

type OpsResources struct {
	MonitorEnabled bool       `json:"monitor_enabled"`
	CPU            OpsPercent `json:"cpu"`
	Memory         OpsUsage   `json:"memory"`
	Disk           OpsUsage   `json:"disk"`
	Network        OpsNetwork `json:"network"`
	Process        OpsProcess `json:"process"`
}

type OpsGateway struct {
	Admission          AdmissionMetrics    `json:"admission"`
	WaitBucketBoundsMS []*int64            `json:"wait_bucket_bounds_ms"`
	RetryBudget        RetryBudgetSnapshot `json:"retry_budget"`
	UserQueue          GatewayQueueMetrics `json:"user_queue"`
	ChannelQueue       GatewayQueueMetrics `json:"channel_queue"`
	SuccessRate        *float64            `json:"success_rate"`
	GrantedPerMinute   *float64            `json:"granted_per_minute"`
	RejectedPerMinute  *float64            `json:"rejected_per_minute"`
}

// Probe errors carry only a category ("timeout" or "unavailable"); raw driver
// errors can name hosts and users and stay in the server log.
type OpsDatabaseProbe struct {
	Kind      string    `json:"kind"`
	Status    OpsStatus `json:"status"`
	LatencyMs *float64  `json:"latency_ms"`
	Error     string    `json:"error"`
	Open      int       `json:"open"`
	InUse     int       `json:"in_use"`
	Idle      int       `json:"idle"`
	MaxOpen   int       `json:"max_open"`
	WaitCount int64     `json:"wait_count"`
}

type OpsRedisProbe struct {
	Enabled    bool      `json:"enabled"`
	Status     OpsStatus `json:"status"`
	LatencyMs  *float64  `json:"latency_ms"`
	Error      string    `json:"error"`
	TotalConns uint32    `json:"total_conns"`
	IdleConns  uint32    `json:"idle_conns"`
	StaleConns uint32    `json:"stale_conns"`
}

type OpsStorage struct {
	Database    OpsDatabaseProbe  `json:"database"`
	LogDatabase *OpsDatabaseProbe `json:"log_database"`
	Redis       OpsRedisProbe     `json:"redis"`
}

type OpsSeriesPoint struct {
	Ts                int64    `json:"ts"`
	CPU               *float64 `json:"cpu"`
	Memory            *float64 `json:"memory"`
	Disk              *float64 `json:"disk"`
	NetUpMbps         *float64 `json:"net_up_mbps"`
	NetDownMbps       *float64 `json:"net_down_mbps"`
	Goroutines        *float64 `json:"goroutines"`
	HeapAllocBytes    *float64 `json:"heap_alloc_bytes"`
	InFlight          *float64 `json:"in_flight"`
	Queued            *float64 `json:"queued"`
	RequestsPerMinute *float64 `json:"requests_per_minute"`
	SuccessRate       *float64 `json:"success_rate"`
	GrantedPerMinute  *float64 `json:"granted_per_minute"`
	RejectedPerMinute *float64 `json:"rejected_per_minute"`
}

type OpsSeries struct {
	Window        string           `json:"window"`
	BucketSeconds int64            `json:"bucket_seconds"`
	Points        []OpsSeriesPoint `json:"points"`
}

type OpsOverview struct {
	SchemaVersion int          `json:"schema_version"`
	SampledAt     int64        `json:"sampled_at"`
	Status        OpsStatus    `json:"status"`
	Checks        []OpsCheck   `json:"checks"`
	Runtime       OpsRuntime   `json:"runtime"`
	Resources     OpsResources `json:"resources"`
	Gateway       OpsGateway   `json:"gateway"`
	Storage       OpsStorage   `json:"storage"`
	Series        OpsSeries    `json:"series"`
}
