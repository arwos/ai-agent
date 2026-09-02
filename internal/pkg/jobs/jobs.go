/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package jobs provides cancellable background job supervision.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.osspkg.com/logx"
)

type Mode uint8

const (
	Once Mode = iota
	Forever
)

type Options struct {
	Mode       Mode
	RetryDelay time.Duration
	MaxRetries int
}

type Handler func(context.Context) error

type Job struct {
	name   string
	opts   Options
	handle Handler
	parent context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func New() *Registry { return &Registry{jobs: make(map[string]*Job)} }

func (r *Registry) Register(name string, opts Options, handler Handler) error {
	if name == "" {
		return errors.New("job name is required")
	}
	if handler == nil {
		return fmt.Errorf("job %q handler is required", name)
	}
	if opts.Mode != Once && opts.Mode != Forever {
		return fmt.Errorf("job %q has unsupported mode", name)
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = time.Second
	}
	if opts.MaxRetries < 0 {
		return fmt.Errorf("job %q max retries cannot be negative", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[name]; exists {
		return fmt.Errorf("job %q is already registered", name)
	}
	r.jobs[name] = &Job{name: name, opts: opts, handle: handler}
	return nil
}

func (r *Registry) Start(ctx context.Context, name string) error {
	r.mu.Lock()
	job := r.jobs[name]
	if job == nil {
		r.mu.Unlock()
		return fmt.Errorf("job %q is not registered", name)
	}
	if job.cancel != nil {
		r.mu.Unlock()
		return fmt.Errorf("job %q is already running", name)
	}
	jobCtx, cancel := context.WithCancel(ctx)
	job.parent = ctx
	job.cancel = cancel
	job.done = make(chan struct{})
	done := job.done
	r.mu.Unlock()
	logx.Info("background job started", "job", name)
	go r.run(jobCtx, job, done)
	return nil
}

func (r *Registry) Restart(name string) error {
	r.mu.Lock()
	job := r.jobs[name]
	if job == nil {
		r.mu.Unlock()
		return fmt.Errorf("job %q is not registered", name)
	}
	parent := job.parent
	cancel := job.cancel
	done := job.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if parent == nil {
		return fmt.Errorf("job %q has not been started", name)
	}
	return r.Start(parent, name)
}

func (r *Registry) Cancel(name string) error {
	r.mu.Lock()
	job := r.jobs[name]
	if job == nil {
		r.mu.Unlock()
		return fmt.Errorf("job %q is not registered", name)
	}
	if job.cancel == nil {
		r.mu.Unlock()
		return nil
	}
	job.cancel()
	r.mu.Unlock()
	logx.Info("background job stopped", "job", name)
	return nil
}

func (r *Registry) run(ctx context.Context, job *Job, done chan struct{}) {
	defer func() {
		r.mu.Lock()
		if job.done == done {
			job.cancel = nil
			job.done = nil
		}
		r.mu.Unlock()
		close(done)
	}()

	retries := 0
	for {
		err := job.handle(ctx)
		if err == nil {
			if job.opts.Mode == Once {
				return
			}
			if !wait(ctx, job.opts.RetryDelay) {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		logx.Error("background job failed", "job", job.name, "attempt", retries+1, "err", err)
		if job.opts.Mode == Once && retries >= job.opts.MaxRetries {
			return
		}
		retries++
		if !wait(ctx, job.opts.RetryDelay) {
			return
		}
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
