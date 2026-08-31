package prometheus

import "fmt"

// The HTTP server metric family, named as VictoriaMetrics renders the
// OpenTelemetry semconv histogram http.server.request.duration under
// usePrometheusNaming (RFC-0014).
const (
	httpDurationCount  = "http_server_request_duration_seconds_count"
	httpDurationBucket = "http_server_request_duration_seconds_bucket"
	httpDurationSum    = "http_server_request_duration_seconds_sum"
)

// HTTPRequestRate is requests per second, grouped by route.
func HTTPRequestRate(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) by (http_route)`,
		httpDurationCount, s.Matchers(), rateInterval)
}

// HTTPErrorRate is failing requests per second, grouped by route.
//
// 5xx only: a 4xx is the caller's problem and putting it here makes every
// error panel alarming for reasons nobody on call can act on.
func HTTPErrorRate(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) by (http_route)`,
		httpDurationCount, s.Matchers(`http_response_status_code=~"5.."`), rateInterval)
}

// HTTPErrorRatio is the fraction of requests failing, between 0 and 1.
//
// The denominator carries a `> 0` guard: without it a service with no traffic
// divides by zero and reports NaN, which a threshold comparison treats as "not
// breaching" and an alert therefore never fires on.
func HTTPErrorRatio(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) / (sum(rate(%s%s[%s])) > 0)`,
		httpDurationCount, s.Matchers(`http_response_status_code=~"5.."`), rateInterval,
		httpDurationCount, s.Matchers(), rateInterval)
}

// HTTPLatencyQuantile is the requested latency quantile in seconds, by route.
//
// `le` stays in the inner aggregation: dropping it makes histogram_quantile
// return NaN, which is the single most common way this query is written wrong.
func HTTPLatencyQuantile(s Selector, quantile float64) string {
	return fmt.Sprintf(`histogram_quantile(%g, sum(rate(%s%s[%s])) by (le, http_route))`,
		quantile, httpDurationBucket, s.Matchers(), rateInterval)
}

// HTTPLatencyAverage is mean latency in seconds, by route.
func HTTPLatencyAverage(s Selector) string {
	return fmt.Sprintf(`sum(rate(%s%s[%s])) by (http_route) / (sum(rate(%s%s[%s])) by (http_route) > 0)`,
		httpDurationSum, s.Matchers(), rateInterval,
		httpDurationCount, s.Matchers(), rateInterval)
}
