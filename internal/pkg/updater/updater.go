/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package updater checks GitHub releases and safely replaces the running binary.
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arwos/ai-agent/internal/pkg/version"
)

const apiURL = "https://api.github.com/repos/arwos/ai-agent/releases/latest"

type Release struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"publishedAt"`
	Available   bool   `json:"available"`
	AssetURL    string `json:"-"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type Service struct {
	mu      sync.RWMutex
	release Release
	client  *http.Client
}

func New() *Service { return &Service{client: &http.Client{Timeout: 15 * time.Second}} }

func (s *Service) Check(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arwos-agent-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub release check: %s", resp.Status)
	}
	var src githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&src); err != nil {
		return Release{}, err
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	assetName := fmt.Sprintf("arwos-agent-%s-%s.%s", runtime.GOOS, runtime.GOARCH, ext)
	r := Release{Version: src.TagName, Name: src.Name, Body: src.Body, PublishedAt: src.PublishedAt, Available: newer(src.TagName, version.Value)}
	for _, asset := range src.Assets {
		if asset.Name == assetName {
			r.AssetURL = asset.URL
			break
		}
	}
	if r.Available && r.AssetURL == "" {
		r.Available = false
	}
	s.mu.Lock()
	s.release = r
	s.mu.Unlock()
	return r, nil
}

func (s *Service) Latest() Release { s.mu.RLock(); defer s.mu.RUnlock(); return s.release }

func newer(candidate, current string) bool {
	if strings.Contains(current, "-dev") {
		return candidate != ""
	}
	parse := func(value string) ([3]int, bool) {
		var out [3]int
		parts := strings.Split(strings.TrimPrefix(strings.SplitN(value, "-", 2)[0], "v"), ".")
		if len(parts) != 3 {
			return out, false
		}
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	a, okA := parse(candidate)
	b, okB := parse(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Prepare downloads and extracts the platform archive. The caller launches the
// returned script only after its RPC response is sent to the browser.
func (s *Service) Prepare(ctx context.Context) (string, error) {
	r := s.Latest()
	if !r.Available || r.AssetURL == "" {
		return "", fmt.Errorf("no compatible update is available")
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "arwos-update-")
	if err != nil {
		return "", err
	}
	archive := filepath.Join(dir, "release")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "arwos-agent-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: %s", resp.Status)
	}
	f, err := os.Create(archive)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 300<<20))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	binary := filepath.Join(dir, "arwos-agent")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := extract(archive, binary); err != nil {
		return "", err
	}
	return updateScript(dir, self, binary), nil
}

func extract(archive, target string) error {
	if runtime.GOOS == "windows" {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			if filepath.Base(f.Name) != "arwos-agent.exe" {
				continue
			}
			in, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
			if err == nil {
				_, err = io.Copy(out, in)
				out.Close()
			}
			in.Close()
			return err
		}
		return fmt.Errorf("release archive has no executable")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) == "arwos-agent" {
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			cerr := out.Close()
			if err != nil {
				return err
			}
			return cerr
		}
	}
	return fmt.Errorf("release archive has no executable")
}
