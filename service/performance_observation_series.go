package service

import "time"

type trafficSampler struct {
	lastAt            time.Time
	request, response uint64
}

// The first sample, counter reset, clock reversal or a sampling gap establishes
// a new baseline. A valid zero delta is genuinely idle HTTP traffic.
func (s *trafficSampler) sample(now time.Time, request, response uint64) ObservationTraffic {
	result := ObservationTraffic{Scope: "application_http_body"}
	elapsed := now.Sub(s.lastAt)
	if !s.lastAt.IsZero() && elapsed > 0 && elapsed <= observationMaxAge && request >= s.request && response >= s.response {
		result.RequestMbps = observationPtr(float64(request-s.request) / elapsed.Seconds() * 8 / 1e6)
		result.ResponseMbps = observationPtr(float64(response-s.response) / elapsed.Seconds() * 8 / 1e6)
	}
	s.lastAt, s.request, s.response = now, request, response
	return result
}

func buildObservationSeries(points []ObservationSeriesPoint, window string, since, until, bucket int64) ObservationSeries {
	first := since - since%bucket
	last := until - until%bucket
	series := ObservationSeries{Window: window, BucketSeconds: bucket, Points: make([]ObservationSeriesPoint, (last-first)/bucket+1)}
	type aggregate struct {
		sums   [7]float64
		counts [7]int
	}
	aggregates := make([]aggregate, len(series.Points))
	for i := range series.Points {
		series.Points[i].Ts = first + int64(i)*bucket
	}
	for _, point := range points {
		if point.Ts < since || point.Ts > until {
			continue
		}
		i := int((point.Ts - first) / bucket)
		values := [7]*float64{point.CPU, point.Memory, point.Disk, point.RequestMbps, point.ResponseMbps, point.Goroutines, point.HeapAllocBytes}
		for field, value := range values {
			if value != nil {
				aggregates[i].sums[field] += *value
				aggregates[i].counts[field]++
			}
		}
	}
	for i, aggregate := range aggregates {
		point := &series.Points[i]
		fields := [7]**float64{&point.CPU, &point.Memory, &point.Disk, &point.RequestMbps, &point.ResponseMbps, &point.Goroutines, &point.HeapAllocBytes}
		for field, count := range aggregate.counts {
			if count > 0 {
				*fields[field] = observationPtr(aggregate.sums[field] / float64(count))
			}
		}
	}
	return series
}
