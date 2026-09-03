/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const BufferSize = 10 * 1024 * 1024

type Progress func(percent float64)

type Client struct{ HTTP *http.Client }

func New(httpClient *http.Client) *Client { return &Client{HTTP: httpClient} }

func (c *Client) Fetch(ctx context.Context, url string, limit int64, progress Progress) ([]byte, error) {
	var data bytes.Buffer
	if err := c.stream(ctx, url, limit, progress, &data); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func (c *Client) Download(ctx context.Context, url, target string, limit int64, progress Progress) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".arwos-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // cleanup errors cannot be returned from this scope
	if err := c.stream(ctx, url, limit, progress, tmp); err != nil {
		tmp.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

func (c *Client) stream(ctx context.Context, url string, limit int64, progress Progress, dst io.Writer) error {
	if progress == nil {
		progress = func(float64) {}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token := os.Getenv("HF_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			lastErr = requestErr
		} else if resp.StatusCode == http.StatusOK {
			reader := &reader{source: io.LimitReader(resp.Body, limit), total: resp.ContentLength, report: progress}
			_, lastErr = io.CopyBuffer(dst, reader, make([]byte, BufferSize))
			_ = resp.Body.Close()
			if lastErr == nil {
				return nil
			}
			// Retrying after a partial copy would append a second, corrupt body to
			// dst. Callers can retry the whole operation with a fresh destination.
			return lastErr
		} else {
			lastErr = fmt.Errorf("%s: %s", url, resp.Status)
			_ = resp.Body.Close()
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		}
		if attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

type reader struct {
	source      io.Reader
	total, read int64
	report      Progress
}

func (r *reader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.read += int64(n)
	if r.total > 0 {
		r.report(float64(r.read) * 100 / float64(r.total))
	}
	return n, err
}
