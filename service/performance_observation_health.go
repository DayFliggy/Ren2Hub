package service

func observationHealth(observation PerformanceObservation) ([]ObservationCheck, ObservationCoverage, string) {
	checks := make([]ObservationCheck, 0, 6)
	for _, item := range []struct {
		id       string
		resource ObservationResource
	}{
		{"cpu", observation.Resources.CPU}, {"memory", observation.Resources.Memory}, {"disk", observation.Resources.Disk},
	} {
		checks = append(checks, observationResourceCheck(item.id, item.resource))
	}
	checks = append(checks, ObservationCheck{ID: "database", Status: observation.Storage.Database.Status, Reason: observation.Storage.Database.Reason})
	if observation.Storage.LogDatabase != nil {
		checks = append(checks, ObservationCheck{ID: "log_database", Status: observation.Storage.LogDatabase.Status, Reason: observation.Storage.LogDatabase.Reason})
	}
	checks = append(checks, ObservationCheck{ID: "redis", Status: observation.Storage.Redis.Status, Reason: observation.Storage.Redis.Reason})
	coverage := ObservationCoverage{}
	overall := "ok"
	unknown := false
	for _, check := range checks {
		if check.Status == "disabled" {
			continue
		}
		coverage.Total++
		switch check.Status {
		case "ok", "warning", "critical":
			coverage.Measured++
			if check.Status == "critical" {
				overall = "critical"
			}
			if check.Status == "warning" && overall != "critical" {
				overall = "warning"
			}
		default:
			unknown = true
		}
	}
	if observation.Stale {
		overall = "stale"
	} else if overall == "ok" && (unknown || coverage.Total == 0) {
		overall = "unknown"
	}
	return checks, coverage, overall
}

func observationResourceCheck(id string, resource ObservationResource) ObservationCheck {
	check := ObservationCheck{ID: id, Status: resource.State}
	// Disabling a threshold disables this check, not the underlying measurement.
	if resource.Threshold <= 0 {
		check.Status, check.Reason = "disabled", "disabled"
		return check
	}
	switch resource.State {
	case "ok":
		if resource.Percent != nil && int(*resource.Percent) > resource.Threshold {
			check.Status, check.Reason = "warning", "threshold_exceeded"
		}
	case "disabled", "stale":
		check.Reason = resource.State
	default:
		check.Reason = "unavailable"
		if resource.SampledAt == nil {
			check.Reason = "not_sampled"
		}
	}
	return check
}
