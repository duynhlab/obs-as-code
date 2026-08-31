package prometheus

import "fmt"

// Go runtime metrics, as exported by the standard Prometheus client.
const (
	goGoroutines     = "go_goroutines"
	goHeapAlloc      = "go_memstats_heap_alloc_bytes"
	goGCDuration     = "go_gc_duration_seconds_sum"
	processCPU       = "process_cpu_seconds_total"
	processStartTime = "process_start_time_seconds"
)

// Goroutines is the live goroutine count, by instance.
func Goroutines(s Selector) string {
	return fmt.Sprintf(`sum(%s%s) by (instance)`, goGoroutines, s.Matchers())
}

// HeapAllocBytes is bytes of allocated heap still in use, by instance.
func HeapAllocBytes(s Selector) string {
	return fmt.Sprintf(`sum(%s%s) by (instance)`, goHeapAlloc, s.Matchers())
}

// GCTimeRatio is the fraction of wall time spent in garbage collection.
func GCTimeRatio(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) by (instance)`, goGCDuration, s.Matchers(), rateInterval)
}

// CPUSeconds is CPU seconds consumed per second, by instance.
func CPUSeconds(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) by (instance)`, processCPU, s.Matchers(), rateInterval)
}

// Restarts counts process starts over the dashboard's time range, by instance.
//
// changes() over process_start_time_seconds rather than a container restart
// counter, so the query works for anything exporting the standard process
// collector, in or out of Kubernetes.
func Restarts(s Selector) string {
	return fmt.Sprintf(`changes(%s%s[$__range])`, processStartTime, s.Matchers())
}

// Up reports whether targets are being scraped, by instance.
func Up(s Selector) string {
	return fmt.Sprintf(`up%s`, s.Matchers())
}
