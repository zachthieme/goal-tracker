package web_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// metricsServer is a Server over h's Service that records its requests in the
// returned metrics. Tests call its ServeHTTP directly, so a request is
// recorded once the call returns.
func metricsServer(t *testing.T, h *testsupport.Harness) (*web.Server, *web.RequestMetrics) {
	t.Helper()
	m := web.NewRequestMetrics()
	return web.NewServer(h.Service, web.WithRequestMetrics(m)), m
}

// promSample is one sample line of a scrape: a metric's name, its labels and
// its value.
type promSample struct {
	name   string
	labels map[string]string
	value  float64
}

// scrape GETs /metrics from m, checks it answers 200 with the Prometheus text
// format's content type, and returns its samples in the order written, as
// parsePromText reads them.
func scrape(t *testing.T, m *web.RequestMetrics) []promSample {
	t.Helper()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("GET /metrics: Content-Type %q, want the Prometheus text format's, version 0.0.4", ct)
	}
	return parsePromText(t, rec.Body.String())
}

var (
	promMetricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	promLabelName  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	promTypes      = []string{"counter", "gauge", "histogram", "summary", "untyped"}
)

// parsePromText reads body as the Prometheus text exposition format, version
// 0.0.4, line by line, failing t on a line that isn't well formed: each
// metric's # HELP and # TYPE come once, before its samples; each sample is a
// metric name, its labels in braces, each value quoted with \\, \" and \n the
// only escapes, and a value; and the text ends in a newline. A histogram's
// samples are its name with _bucket, _sum or _count. It returns the samples
// in the order written.
func parsePromText(t *testing.T, body string) []promSample {
	t.Helper()
	if !strings.HasSuffix(body, "\n") {
		t.Errorf("scrape doesn't end in a newline:\n%s", body)
	}
	helped, typed := map[string]bool{}, map[string]string{}
	sampled := map[string]bool{}
	var samples []promSample
	for i, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		n := i + 1
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			keyword, rest, _ := strings.Cut(rest, " ")
			name, text, _ := strings.Cut(rest, " ")
			switch keyword {
			case "HELP":
				if helped[name] || sampled[name] {
					t.Errorf("line %d: # HELP for %s repeated or after its samples: %q", n, name, line)
				}
				helped[name] = true
			case "TYPE":
				if typed[name] != "" || sampled[name] || !slices.Contains(promTypes, text) {
					t.Errorf("line %d: # TYPE for %s repeated, after its samples, or of no type: %q", n, name, line)
				}
				typed[name] = text
			default:
				continue // a comment
			}
			if !promMetricName.MatchString(name) {
				t.Errorf("line %d: %q names no metric", n, line)
			}
			continue
		}
		s, ok := parsePromSample(line)
		if !ok {
			t.Errorf("line %d: %q isn't a well-formed sample", n, line)
			continue
		}
		family := s.name
		if typed[family] == "" {
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				if base, ok := strings.CutSuffix(s.name, suffix); ok && typed[base] == "histogram" {
					family = base
				}
			}
		}
		if typed[family] == "" || !helped[family] {
			t.Errorf("line %d: sample %q comes before its metric's # HELP and # TYPE", n, line)
		}
		if typed[family] == "histogram" && family == s.name {
			t.Errorf("line %d: histogram sample %q isn't its _bucket, _sum or _count", n, line)
		}
		sampled[family] = true
		samples = append(samples, s)
	}
	return samples
}

// parsePromSample reads a sample line: name{label="value",...} value.
func parsePromSample(line string) (promSample, bool) {
	s := promSample{labels: map[string]string{}}
	end := strings.IndexAny(line, "{ ")
	if end < 0 {
		return s, false
	}
	s.name, line = line[:end], line[end:]
	if !promMetricName.MatchString(s.name) {
		return s, false
	}
	if rest, ok := strings.CutPrefix(line, "{"); ok {
		for {
			if after, ok := strings.CutPrefix(rest, "}"); ok {
				rest = after
				break
			}
			name, after, ok := strings.Cut(rest, `="`)
			if !ok || !promLabelName.MatchString(name) {
				return s, false
			}
			var value strings.Builder
			closed := false
			for i := 0; i < len(after); i++ {
				switch c := after[i]; c {
				case '\\':
					if i+1 == len(after) {
						return s, false
					}
					i++
					switch after[i] {
					case '\\':
						value.WriteByte('\\')
					case '"':
						value.WriteByte('"')
					case 'n':
						value.WriteByte('\n')
					default:
						return s, false
					}
				case '"':
					rest, closed = after[i+1:], true
				case '\n':
					return s, false
				default:
					value.WriteByte(c)
				}
				if closed {
					break
				}
			}
			if !closed {
				return s, false
			}
			if _, dup := s.labels[name]; dup {
				return s, false
			}
			s.labels[name] = value.String()
			rest, _ = strings.CutPrefix(rest, ",")
		}
		line = rest
	}
	value, ok := strings.CutPrefix(line, " ")
	if !ok || strings.Contains(value, " ") {
		return s, false
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return s, false
	}
	s.value = v
	return s, true
}

// samplesOf are the samples named name whose labels include labels, given as
// name, value pairs, in the order written.
func samplesOf(samples []promSample, name string, labels ...string) []promSample {
	var found []promSample
	for _, s := range samples {
		if s.name != name {
			continue
		}
		match := true
		for i := 0; i+1 < len(labels); i += 2 {
			if v, ok := s.labels[labels[i]]; !ok || v != labels[i+1] {
				match = false
			}
		}
		if match {
			found = append(found, s)
		}
	}
	return found
}

// sampleValue is the value of the one sample named name with exactly labels,
// given as name, value pairs; it fails t when there isn't exactly one.
func sampleValue(t *testing.T, samples []promSample, name string, labels ...string) float64 {
	t.Helper()
	var found []promSample
	for _, s := range samplesOf(samples, name, labels...) {
		if len(s.labels) == len(labels)/2 {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("scrape has %d samples of %s%v, want 1: %v", len(found), name, labels, samples)
	}
	return found[0].value
}

// Each request is counted by its method, the route it matched and its status,
// and timed by its route, under the pattern its access line names, not its
// path, so IDs and mistyped paths don't grow the series without bound: a path
// no route serves is counted as unmatched, with the 404 the catch-all answers
// it, so it doesn't share the index page's series.
func TestRequestsAreCountedAndTimedByRouteAndStatus(t *testing.T) {
	t.Parallel()

	srv, m := metricsServer(t, testsupport.New(t))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/no-such-page", nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	samples := scrape(t, m)
	const requests, duration = "goal_tracker_http_requests_total", "goal_tracker_http_request_duration_seconds"
	if n := sampleValue(t, samples, requests, "method", "GET", "route", "GET /healthz", "status", "200"); n != 2 {
		t.Errorf("GET /healthz answered 200 counted %v times, want 2", n)
	}
	if n := sampleValue(t, samples, requests, "method", "GET", "route", "unmatched", "status", "404"); n != 1 {
		t.Errorf("the unknown path, answered 404, counted as unmatched %v times, want 1", n)
	}
	if n := sampleValue(t, samples, requests, "method", "GET", "route", "GET /", "status", "303"); n != 1 {
		t.Errorf("the index page, answered 303, counted under GET / %v times, want 1", n)
	}
	if got := samplesOf(samples, requests, "route", "GET /", "status", "404"); len(got) != 0 {
		t.Errorf("a 404 counted under the index page's GET /: %v", got)
	}
	if n := sampleValue(t, samples, duration+"_count", "route", "GET /"); n != 1 {
		t.Errorf("GET / timed %v times, want only the index page's request", n)
	}
	for _, s := range samples {
		for _, v := range s.labels {
			if strings.Contains(v, "no-such-page") {
				t.Errorf("sample %v is labelled with the unknown path", s)
			}
		}
	}

	if n := sampleValue(t, samples, duration+"_count", "route", "GET /healthz"); n != 2 {
		t.Errorf("GET /healthz timed %v times, want 2", n)
	}
	if sum := sampleValue(t, samples, duration+"_sum", "route", "GET /healthz"); sum <= 0 {
		t.Errorf("GET /healthz took %v seconds in all, want some time", sum)
	}
	buckets := samplesOf(samples, duration+"_bucket", "route", "GET /healthz")
	if len(buckets) < 2 {
		t.Fatalf("GET /healthz's duration has %d buckets, want several", len(buckets))
	}
	prevLE, prev := 0.0, 0.0
	for _, b := range buckets {
		le, err := strconv.ParseFloat(b.labels["le"], 64)
		if err != nil || le <= prevLE || b.value < prev {
			t.Errorf("bucket %v doesn't follow le=%v holding %v: buckets are cumulative, by rising le", b, prevLE, prev)
		}
		prevLE, prev = le, b.value
	}
	if last := buckets[len(buckets)-1]; last.labels["le"] != "+Inf" || last.value != 2 {
		t.Errorf("the last bucket is %v, want le=+Inf holding both requests", last)
	}
}

// The scrape is Prometheus's text format, version 0.0.4, which parsePromText
// checks line by line, before any request and after: a route's quote and
// backslash are escaped, so its pattern reads back whole, and a method the
// app doesn't know is counted as OTHER, so a client can't add series.
func TestTheScrapeIsPrometheusTextFormat(t *testing.T) {
	t.Parallel()

	srv, m := metricsServer(t, testsupport.New(t))
	if samples := scrape(t, m); len(samples) != 0 {
		t.Errorf("before any request the scrape has samples %v, want none", samples)
	}

	const odd = `GET /say/"hi"\there`
	srv.HandleForPanicTest(odd, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, `/say/%22hi%22%5Cthere`, nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), "BREW", "/healthz", nil))

	samples := scrape(t, m)
	if n := sampleValue(t, samples, "goal_tracker_http_requests_total", "method", "GET", "route", odd, "status", "418"); n != 1 {
		t.Errorf("%s counted %v times, want 1", odd, n)
	}
	if n := sampleValue(t, samples, "goal_tracker_http_request_duration_seconds_count", "route", odd); n != 1 {
		t.Errorf("%s timed %v times, want 1", odd, n)
	}
	if got := samplesOf(samples, "goal_tracker_http_requests_total", "method", "OTHER"); len(got) != 1 || got[0].value != 1 {
		t.Errorf("BREW counted as %v, want once as OTHER", got)
	}
	if got := samplesOf(samples, "goal_tracker_http_requests_total", "method", "BREW"); len(got) != 0 {
		t.Errorf("BREW counted under its own method: %v", got)
	}
}

// Requests answered at once, while the metrics are scraped, are each counted
// once: recording is safe under concurrent requests, as -race checks. They ask
// for a static file, which needs no database, so each answers 200 however busy
// the machine is; /healthz answers 503 when its ping outlasts its timeout.
func TestConcurrentRequestsAreEachCounted(t *testing.T) {
	t.Parallel()

	srv, m := metricsServer(t, testsupport.New(t))
	const workers, each = 16, 10
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range each {
				srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/static/app.css", nil))
			}
		})
	}
	wg.Go(func() {
		for range each {
			m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
		}
	})
	wg.Wait()

	samples := scrape(t, m)
	if n := sampleValue(t, samples, "goal_tracker_http_requests_total", "method", "GET", "route", "GET /static/", "status", "200"); n != workers*each {
		t.Errorf("%d requests at once counted %v, want %d", workers*each, n, workers*each)
	}
	if n := sampleValue(t, samples, "goal_tracker_http_request_duration_seconds_count", "route", "GET /static/"); n != workers*each {
		t.Errorf("%d requests at once timed %v, want %d", workers*each, n, workers*each)
	}
}
