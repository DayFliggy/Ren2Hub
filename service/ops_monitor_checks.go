package service

const (
	// A resource within this many points of its 503 threshold is a warning.
	opsResourceWarningMargin = 10
	// The relay success rate counts 4xx responses as failures, so it cannot
	// tell client mistakes from upstream outages. It warns, never escalates.
	opsSuccessRateHealthy = 99
)

// deriveOpsChecks grades the sampled state on the server so the dashboard never
// re-implements thresholds.
func deriveOpsChecks(resources OpsResources, gateway OpsGateway, storage OpsStorage, master bool) []OpsCheck {
	checks := []OpsCheck{
		opsResourceCheck("cpu", resources.MonitorEnabled, resources.CPU.Percent, resources.CPU.Threshold),
		opsResourceCheck("memory", resources.MonitorEnabled, resources.Memory.Percent, resources.Memory.Threshold),
		opsResourceCheck("disk", resources.MonitorEnabled, resources.Disk.Percent, resources.Disk.Threshold),
		{Key: "success_rate", Status: opsSuccessRateStatus(gateway.SuccessRate), Value: gateway.SuccessRate},
		{Key: "admission", Status: opsRejectionStatus(gateway.RejectedPerMinute), Value: gateway.RejectedPerMinute},
		{Key: "draining", Status: opsWarnIf(gateway.Admission.Draining)},
		// Scheduled maintenance, polling and billing recovery only run on master.
		{Key: "master", Status: opsWarnIf(!master)},
		opsDatabaseCheck("database", storage.Database),
	}
	if storage.LogDatabase != nil {
		check := opsDatabaseCheck("log_database", *storage.LogDatabase)
		// Consumption logs wait in the billing outbox, so a log store outage
		// delays logs without stopping relay or settlement.
		if check.Status == OpsStatusCritical {
			check.Status = OpsStatusWarning
		}
		checks = append(checks, check)
	}
	if storage.Redis.Enabled {
		checks = append(checks, OpsCheck{Key: "redis", Status: storage.Redis.Status, Value: storage.Redis.LatencyMs, Error: storage.Redis.Error})
	}
	return checks
}

func opsDatabaseCheck(key string, probe OpsDatabaseProbe) OpsCheck {
	return OpsCheck{Key: key, Status: probe.Status, Value: probe.LatencyMs, Error: probe.Error}
}

// opsResourceCheck mirrors SystemPerformanceCheck, which refuses relay
// requests once the truncated percentage exceeds a positive threshold.
func opsResourceCheck(key string, enabled bool, percent *float64, threshold int) OpsCheck {
	check := OpsCheck{Key: key, Status: OpsStatusUnknown, Value: percent, Threshold: &threshold}
	if !enabled || percent == nil {
		return check
	}
	switch value := int(*percent); {
	case threshold <= 0:
		check.Status = OpsStatusOK
	case value > threshold:
		check.Status = OpsStatusCritical
	case value > threshold-opsResourceWarningMargin:
		check.Status = OpsStatusWarning
	default:
		check.Status = OpsStatusOK
	}
	return check
}

func opsSuccessRateStatus(rate *float64) OpsStatus {
	if rate == nil {
		return OpsStatusUnknown
	}
	if *rate < opsSuccessRateHealthy {
		return OpsStatusWarning
	}
	return OpsStatusOK
}

// Any gateway-side refusal (queue full or wait timeout) in the last minute
// means demand exceeded admission capacity.
func opsRejectionStatus(perMinute *float64) OpsStatus {
	if perMinute == nil {
		return OpsStatusUnknown
	}
	if *perMinute > 0 {
		return OpsStatusWarning
	}
	return OpsStatusOK
}

func opsWarnIf(condition bool) OpsStatus {
	if condition {
		return OpsStatusWarning
	}
	return OpsStatusOK
}

// rollupOpsStatus reports the worst measured check. It is unknown only when
// nothing was measured.
func rollupOpsStatus(checks []OpsCheck) OpsStatus {
	rank := map[OpsStatus]int{OpsStatusUnknown: 0, OpsStatusOK: 1, OpsStatusWarning: 2, OpsStatusCritical: 3}
	worst := OpsStatusUnknown
	for _, check := range checks {
		if rank[check.Status] > rank[worst] {
			worst = check.Status
		}
	}
	return worst
}
