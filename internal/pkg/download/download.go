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
	defer os.Remove(tmpName)
	if err := c.stream(ctx, url, limit, progress, tmp); err != nil {
		tmp.Close()
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
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	reader := &reader{source: io.LimitReader(resp.Body, limit), total: resp.ContentLength, report: progress}
	_, err = io.CopyBuffer(dst, reader, make([]byte, BufferSize))
	return err
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
