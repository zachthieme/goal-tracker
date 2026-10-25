package web

import (
	"bufio"
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestMetrics counts and times the requests a Server answers (see
// WithRequestMetrics), and serves them to Prometheus in its text format (see
// ServeHTTP). Each request is labelled by the route pattern its access line
// names, never its path, and by a method from a fixed list, so the series
// can't grow without bound. It is safe to record into and scrape at once.
type RequestMetrics struct {
	mu sync.Mutex
	// requests counts the requests answered, by method, route and status.
	requests map[requestMetricsKey]uint64
	// durations times the requests answered, by route.
	durations map[string]*requestDurations
}

// requestMetricsKey is a series of goal_tracker_http_requests_total.
type requestMetricsKey struct {
	method string
	route  string
	status int
}

// requestDurations is a series of goal_tracker_http_request_duration_seconds:
// how many requests took up to each of requestDurationBuckets, not yet
// cumulative, and how long they took in all.
type requestDurations struct {
	buckets [len(requestDurationBuckets)]uint64
	count   uint64
	sum     float64
}

// requestDurationBuckets are the upper bounds, in seconds, of the duration
// histogram's buckets, below its +Inf.
var requestDurationBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// requestMetricsMethods are the methods a request is counted under as sent;
// any other is counted as OTHER, so a client can't add a series per method it
// makes up.
var requestMetricsMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace,
}

// NewRequestMetrics is a RequestMetrics with nothing recorded yet.
func NewRequestMetrics() *RequestMetrics {
	return &RequestMetrics{
		requests:  map[requestMetricsKey]uint64{},
		durations: map[string]*requestDurations{},
	}
}

// WithRequestMetrics has the Server count and time every request it answers,
// once, in m. Without it, the Server records nothing.
func WithRequestMetrics(m *RequestMetrics) Option {
	return func(s *Server) {
		s.metrics = m
	}
}

// record counts a request with method, answered from route with status, that
// took elapsed.
func (m *RequestMetrics) record(method, route string, status int, elapsed time.Duration) {
	if !slices.Contains(requestMetricsMethods, method) {
		method = "OTHER"
	}
	seconds := elapsed.Seconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[requestMetricsKey{method: method, route: route, status: status}]++
	d := m.durations[route]
	if d == nil {
		d = &requestDurations{}
		m.durations[route] = d
	}
	if i, _ := slices.BinarySearch(requestDurationBuckets[:], seconds); i < len(d.buckets) {
		d.buckets[i]++
	}
	d.count++
	d.sum += seconds
}

// ServeHTTP writes what m has recorded in the Prometheus text exposition
// format, version 0.0.4: the request counts, then the duration histogram, each
// under its # HELP and # TYPE, its series sorted by their labels.
func (m *RequestMetrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	requests := make([]requestMetricsKey, 0, len(m.requests))
	counts := make(map[requestMetricsKey]uint64, len(m.requests))
	for k, n := range m.requests {
		requests = append(requests, k)
		counts[k] = n
	}
	routes := make([]string, 0, len(m.durations))
	durations := make(map[string]requestDurations, len(m.durations))
	for route, d := range m.durations {
		routes = append(routes, route)
		durations[route] = *d
	}
	m.mu.Unlock()

	slices.SortFunc(requests, func(a, b requestMetricsKey) int {
		return cmp.Or(cmp.Compare(a.method, b.method), cmp.Compare(a.route, b.route), cmp.Compare(a.status, b.status))
	})
	slices.Sort(routes)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	out := bufio.NewWriter(w)
	fmt.Fprintln(out, "# HELP goal_tracker_http_requests_total HTTP requests answered, by method, route pattern and status.")
	fmt.Fprintln(out, "# TYPE goal_tracker_http_requests_total counter")
	for _, k := range requests {
		fmt.Fprintf(out, "goal_tracker_http_requests_total{method=%s,route=%s,status=\"%d\"} %d\n",
			promLabelValue(k.method), promLabelValue(k.route), k.status, counts[k])
	}
	fmt.Fprintln(out, "# HELP goal_tracker_http_request_duration_seconds How long HTTP requests took to answer, by route pattern.")
	fmt.Fprintln(out, "# TYPE goal_tracker_http_request_duration_seconds histogram")
	for _, route := range routes {
		d, r := durations[route], promLabelValue(route)
		var cumulative uint64
		for i, le := range requestDurationBuckets {
			cumulative += d.buckets[i]
			fmt.Fprintf(out, "goal_tracker_http_request_duration_seconds_bucket{route=%s,le=\"%s\"} %d\n",
				r, strconv.FormatFloat(le, 'g', -1, 64), cumulative)
		}
		fmt.Fprintf(out, "goal_tracker_http_request_duration_seconds_bucket{route=%s,le=\"+Inf\"} %d\n", r, d.count)
		fmt.Fprintf(out, "goal_tracker_http_request_duration_seconds_sum{route=%s} %s\n", r, strconv.FormatFloat(d.sum, 'g', -1, 64))
		fmt.Fprintf(out, "goal_tracker_http_request_duration_seconds_count{route=%s} %d\n", r, d.count)
	}
	_ = out.Flush()
}

// promLabelValue is v quoted as a label value in the Prometheus text format:
// a backslash, a double quote and a newline escaped.
func promLabelValue(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v) + `"`
}
