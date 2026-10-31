package service

import "time"

type trafficSampler struct {
	lastAt  time.Time
	in, out uint64
}

// sample turns cumulative byte counters into Mbps. A failed read (non-empty
// reason) drops the baseline and yields no rates. The first good sample, a
// counter decrease, clock reversal or a gap over observationMaxAge only sets a
// new baseline, so no rate spans a failure. A real zero delta is idle.
func (s *trafficSampler) sample(now time.Time, in, out uint64, reason string) (inMbps, outMbps *float64) {
	if reason != "" {
		*s = trafficSampler{}
		return nil, nil
	}
	elapsed := now.Sub(s.lastAt)
	if !s.lastAt.IsZero() && elapsed > 0 && elapsed <= observationMaxAge && in >= s.in && out >= s.out {
		inMbps = observationPtr(float64(in-s.in) * 8 / elapsed.Seconds() / 1e6)
		outMbps = observationPtr(float64(out-s.out) * 8 / elapsed.Seconds() / 1e6)
	}
	s.lastAt, s.in, s.out = now, in, out
	return inMbps, outMbps
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
		values := [7]*float64{point.CPU, point.Memory, point.Disk, point.InboundMbps, point.OutboundMbps, point.Goroutines, point.HeapAllocBytes}
		for field, value := range values {
			if value != nil {
				aggregates[i].sums[field] += *value
				aggregates[i].counts[field]++
			}
		}
	}
	for i, aggregate := range aggregates {
		point := &series.Points[i]
		fields := [7]**float64{&point.CPU, &point.Memory, &point.Disk, &point.InboundMbps, &point.OutboundMbps, &point.Goroutines, &point.HeapAllocBytes}
		for field, count := range aggregate.counts {
			if count > 0 {
				*fields[field] = observationPtr(aggregate.sums[field] / float64(count))
			}
		}
	}
	return series
}
