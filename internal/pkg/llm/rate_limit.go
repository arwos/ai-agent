/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package llm

import (
	"context"
	"sync"
	"time"
)

type requestLimiter struct {
	mu       sync.Mutex
	next     time.Time
	rpm      int
	requests []time.Time
}

var requestLimiters = struct {
	sync.Mutex
	items map[string]*requestLimiter
}{items: make(map[string]*requestLimiter)}

func (p OpenAPIProvider) waitRateLimit(ctx context.Context) error {
	limiter := limiterFor(p.rateLimitKey())
	if p.RPM <= 0 {
		limiter.record(time.Now())
		return nil
	}

	interval := time.Minute / time.Duration(p.RPM)
	if interval <= 0 {
		limiter.record(time.Now())
		return nil
	}
	limiter.mu.Lock()
	now := time.Now()
	if limiter.rpm != p.RPM {
		// A changed provider setting must take effect immediately instead of
		// retaining a reservation made under the previous RPM value.
		limiter.rpm, limiter.next = p.RPM, now
	}
	scheduled := limiter.next
	if scheduled.Before(now) {
		scheduled = now
	}
	limiter.next = scheduled.Add(interval)
	limiter.mu.Unlock()

	if delay := time.Until(scheduled); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	limiter.record(time.Now())
	return nil
}

func (p OpenAPIProvider) rateLimitKey() string {
	if p.RateLimitKey != "" {
		return p.RateLimitKey
	}
	return p.Kind + "|" + p.BaseURL
}

func limiterFor(key string) *requestLimiter {
	requestLimiters.Lock()
	defer requestLimiters.Unlock()
	limiter := requestLimiters.items[key]
	if limiter == nil {
		limiter = &requestLimiter{}
		requestLimiters.items[key] = limiter
	}
	return limiter
}

func (limiter *requestLimiter) record(now time.Time) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.requests = append(limiter.requests, now)
	limiter.trim(now)
}

// RequestsPerMinute returns completed request starts during the preceding
// rolling minute. A zero value means the provider has not been used yet.
func RequestsPerMinute(key string) int {
	if key == "" {
		return 0
	}
	limiter := limiterFor(key)
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.trim(time.Now())
	return len(limiter.requests)
}

func (limiter *requestLimiter) trim(now time.Time) {
	threshold := now.Add(-time.Minute)
	index := 0
	for index < len(limiter.requests) && limiter.requests[index].Before(threshold) {
		index++
	}
	if index > 0 {
		limiter.requests = append([]time.Time(nil), limiter.requests[index:]...)
	}
}
