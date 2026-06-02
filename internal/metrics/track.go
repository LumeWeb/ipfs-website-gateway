package metrics

import "github.com/prometheus/client_golang/prometheus"

// MetricTrack tracks duration and errors for single-return functions.
// The result type can be error; if so, errors counter is incremented.
func MetricTrack[T any](observer prometheus.Observer, errors prometheus.Counter, f func() T) T {
	timer := prometheus.NewTimer(observer)
	defer timer.ObserveDuration()

	result := f()

	if e, ok := any(result).(error); ok && e != nil && errors != nil {
		errors.Inc()
	}

	return result
}

// MetricTrackResult tracks duration and errors for functions returning (T, error).
func MetricTrackResult[T any](observer prometheus.Observer, errors prometheus.Counter, f func() (T, error)) (T, error) {
	timer := prometheus.NewTimer(observer)
	defer timer.ObserveDuration()

	result, err := f()
	if err != nil && errors != nil {
		errors.Inc()
	}

	return result, err
}

// MetricTrackGauge tracks duration, errors, and manages a gauge counter.
func MetricTrackGauge[T any](gauge prometheus.Gauge, histogram prometheus.Histogram, errors prometheus.Counter, f func() T) T {
	gauge.Inc()
	defer gauge.Dec()
	return MetricTrack(histogram, errors, f)
}

// MetricTrackCache tracks duration, errors, and cache hits/misses.
func MetricTrackCache[T any](duration prometheus.Histogram, errors, hits, misses prometheus.Counter, f func() (T, bool, error)) (T, error) {
	timer := prometheus.NewTimer(duration)
	defer timer.ObserveDuration()

	result, hit, err := f()
	if err != nil {
		errors.Inc()
	} else if hit {
		hits.Inc()
	} else {
		misses.Inc()
	}

	return result, err
}
