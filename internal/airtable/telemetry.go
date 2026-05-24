package airtable

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

type Telemetry struct {
	StartedAt          time.Time            `json:"started_at"`
	FinishedAt         time.Time            `json:"finished_at,omitempty"`
	Requests           int64                `json:"requests"`
	Downloads          int64                `json:"downloads"`
	DownloadBytes      int64                `json:"download_bytes"`
	RateLimitResponses int64                `json:"rate_limit_responses"`
	RetrySleeps        int64                `json:"retry_sleeps"`
	RetrySleepMillis   int64                `json:"retry_sleep_millis"`
	NetworkErrors      int64                `json:"network_errors"`
	StatusCounts       map[int]int64        `json:"status_counts"`
	ByBase             map[string]BaseStats `json:"by_base"`
	Restrictions       []Restriction        `json:"restrictions,omitempty"`
}

type BaseStats struct {
	Requests           int64 `json:"requests"`
	RateLimitResponses int64 `json:"rate_limit_responses"`
	RetrySleepMillis   int64 `json:"retry_sleep_millis"`
}

type Restriction struct {
	At        time.Time `json:"at"`
	BaseID    string    `json:"base_id,omitempty"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Status    int       `json:"status,omitempty"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

type limiterState struct {
	lastRequest time.Time
	penalty     time.Duration
}

type telemetryRecorder struct {
	mu       sync.Mutex
	data     Telemetry
	limiters map[string]*limiterState
}

func newTelemetryRecorder() *telemetryRecorder {
	return &telemetryRecorder{
		data: Telemetry{
			StartedAt:    time.Now(),
			StatusCounts: map[int]int64{},
			ByBase:       map[string]BaseStats{},
		},
		limiters: map[string]*limiterState{},
	}
}

func (r *telemetryRecorder) snapshot() Telemetry {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := r.data
	cp.StatusCounts = map[int]int64{}
	for k, v := range r.data.StatusCounts {
		cp.StatusCounts[k] = v
	}
	cp.ByBase = map[string]BaseStats{}
	for k, v := range r.data.ByBase {
		cp.ByBase[k] = v
	}
	cp.Restrictions = append([]Restriction(nil), r.data.Restrictions...)
	return cp
}

func (r *telemetryRecorder) finish() Telemetry {
	r.mu.Lock()
	r.data.FinishedAt = time.Now()
	r.mu.Unlock()
	return r.snapshot()
}

func (r *telemetryRecorder) pace(baseID string) {
	if baseID == "" {
		return
	}
	r.mu.Lock()
	state := r.limiters[baseID]
	if state == nil {
		state = &limiterState{}
		r.limiters[baseID] = state
	}
	minGap := 220*time.Millisecond + state.penalty
	sleep := time.Duration(0)
	if !state.lastRequest.IsZero() {
		next := state.lastRequest.Add(minGap)
		if now := time.Now(); now.Before(next) {
			sleep = next.Sub(now)
		}
	}
	state.lastRequest = time.Now().Add(sleep)
	r.mu.Unlock()
	if sleep > 0 {
		time.Sleep(sleep)
	}
}

func (r *telemetryRecorder) observe(baseID, endpoint string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data.Requests++
	r.data.StatusCounts[status]++
	stats := r.data.ByBase[baseID]
	stats.Requests++
	r.data.ByBase[baseID] = stats
	if status == 401 || status == 403 || status == 404 || status == 422 {
		r.data.Restrictions = append(r.data.Restrictions, Restriction{
			At: time.Now(), BaseID: baseID, Endpoint: redactEndpoint(endpoint), Status: status,
			Code: "AIRTABLE_RESTRICTION", Message: "Airtable returned a non-success status that may indicate auth, scope, permission, missing resource, or payload restriction", Retryable: false,
		})
	}
}

func (r *telemetryRecorder) observeNetworkError(baseID, endpoint string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data.NetworkErrors++
	r.data.Restrictions = append(r.data.Restrictions, Restriction{
		At: time.Now(), BaseID: baseID, Endpoint: redactEndpoint(endpoint),
		Code: "NETWORK_ERROR", Message: err.Error(), Retryable: true,
	})
}

func (r *telemetryRecorder) observeRateLimit(baseID, endpoint string, sleep time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data.RateLimitResponses++
	r.data.RetrySleeps++
	r.data.RetrySleepMillis += sleep.Milliseconds()
	stats := r.data.ByBase[baseID]
	stats.RateLimitResponses++
	stats.RetrySleepMillis += sleep.Milliseconds()
	r.data.ByBase[baseID] = stats
	state := r.limiters[baseID]
	if state == nil {
		state = &limiterState{}
		r.limiters[baseID] = state
	}
	if state.penalty < 2*time.Second {
		state.penalty += 100 * time.Millisecond
	}
	r.data.Restrictions = append(r.data.Restrictions, Restriction{
		At: time.Now(), BaseID: baseID, Endpoint: redactEndpoint(endpoint), Status: 429,
		Code: "RATE_LIMITED", Message: "Airtable returned 429; client slept and increased per-base pacing", Retryable: true,
	})
}

func (r *telemetryRecorder) observeDownload(bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data.Downloads++
	r.data.DownloadBytes += bytes
}

func baseIDFromEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "v0" && strings.HasPrefix(parts[1], "app") {
		return parts[1]
	}
	if len(parts) >= 4 && parts[0] == "v0" && parts[1] == "meta" && parts[2] == "bases" {
		return parts[3]
	}
	return ""
}

func redactEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	u.RawQuery = ""
	return u.String()
}
