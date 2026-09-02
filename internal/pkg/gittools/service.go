/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package gittools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

const maxDiffBytes = 1 << 20

type StatusEntry struct {
	Path     string `json:"path"`
	Staged   string `json:"staged"`
	Worktree string `json:"worktree"`
}

type CommitEntry struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Message string    `json:"message"`
}

func (r *Repository) Status() ([]StatusEntry, error) {
	wt, err := r.repo.Worktree()
	if err != nil {
		return nil, err
	}
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	out := make([]StatusEntry, 0, len(status))
	for name, value := range status {
		if value.Staging == git.Unmodified && value.Worktree == git.Unmodified {
			continue
		}
		out = append(out, StatusEntry{Path: name, Staged: string([]byte{byte(value.Staging)}), Worktree: string([]byte{byte(value.Worktree)})})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (r *Repository) Log(ctx context.Context, max int, file string) ([]CommitEntry, error) {
	if max <= 0 {
		max = 20
	}
	if max > 50 {
		max = 50
	}
	if file != "" {
		file = path.Clean(file)
		if file == "." || strings.HasPrefix(file, "../") || path.IsAbs(file) {
			return nil, fmt.Errorf("invalid path")
		}
	}
	opts := &git.LogOptions{}
	if file != "" {
		opts.FileName = &file
	}
	iter, err := r.repo.Log(opts)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := make([]CommitEntry, 0, max)
	for len(out) < max {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, err := iter.Next()
		if err != nil {
			if err == plumbing.ErrObjectNotFound || err == io.EOF {
				break
			}
			return nil, err
		}
		out = append(out, CommitEntry{Hash: commit.Hash.String(), Author: commit.Author.Name, Date: commit.Author.When, Message: strings.TrimSpace(commit.Message)})
	}
	return out, nil
}

func (r *Repository) Diff(ctx context.Context, staged bool, file string) (string, error) {
	if file != "" {
		file = path.Clean(file)
		if file == "." || strings.HasPrefix(file, "../") || path.IsAbs(file) {
			return "", fmt.Errorf("invalid path")
		}
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		return "", err
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	for name := range status {
		if file != "" && name != file {
			continue
		}
		if staged && status[name].Staging == git.Unmodified {
			continue
		}
		if !staged && status[name].Worktree == git.Unmodified {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		oldText, oldErr := r.headFile(name)
		newText, newErr := r.worktreeFile(name)
		if staged {
			newText, newErr = r.indexFile(name)
		} else {
			oldText, oldErr = r.indexFile(name)
			if oldErr != nil && isMissing(oldErr) {
				oldText = ""
			}
		}
		if oldErr != nil && !isMissing(oldErr) {
			return "", oldErr
		}
		if newErr != nil && !isMissing(newErr) {
			return "", newErr
		}
		if oldErr != nil {
			oldText = ""
		}
		if newErr != nil {
			newText = ""
		}
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", name, name, name, name)
		writeLineDiff(&b, oldText, newText)
		if b.Len() >= maxDiffBytes {
			break
		}
	}
	if b.Len() >= maxDiffBytes {
		return b.String()[:maxDiffBytes] + "\n[Diff truncated]", nil
	}
	return b.String(), nil
}

func (r *Repository) indexFile(name string) (string, error) {
	idx, err := r.repo.Storer.Index()
	if err != nil {
		return "", err
	}
	entry, err := idx.Entry(name)
	if err != nil {
		return "", os.ErrNotExist
	}
	blob, err := r.repo.BlobObject(entry.Hash)
	if err != nil {
		return "", err
	}
	reader, err := blob.Reader()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxDiffBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxDiffBytes {
		return "", fmt.Errorf("file %q exceeds diff limit", name)
	}
	return string(data), nil
}

func (r *Repository) headFile(name string) (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return "", err
	}
	file, err := commit.File(name)
	if err != nil {
		return "", err
	}
	return file.Contents()
}

func (r *Repository) worktreeFile(name string) (string, error) {
	f, err := r.worktree.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxDiffBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxDiffBytes {
		return "", fmt.Errorf("file %q exceeds diff limit", name)
	}
	return string(b), nil
}

func isMissing(err error) bool {
	return err != nil && (err == os.ErrNotExist || errors.Is(err, os.ErrNotExist))
}

func writeLineDiff(out *bytes.Buffer, oldText, newText string) {
	if oldText == newText {
		return
	}
	for _, line := range splitLines(oldText) {
		fmt.Fprintf(out, "-%s\n", line)
	}
	for _, line := range splitLines(newText) {
		fmt.Fprintf(out, "+%s\n", line)
	}
}

func splitLines(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(value, "\n"), "\n")
}

func (r *Repository) Add(files []string) error {
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := validateRelative(file); err != nil {
			return err
		}
		if _, err := wt.Add(file); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) Commit(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("commit message is required")
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		return "", err
	}
	// Agent commits are deliberately unsigned and isolated from a user's
	// global commit.gpgSign setting; no signing key is available to the agent.
	if cfg, cfgErr := r.repo.Config(); cfgErr == nil {
		cfg.Commit.GpgSign = config.NewOptBool(false)
		if cfgErr = r.repo.SetConfig(cfg); cfgErr != nil {
			return "", cfgErr
		}
	}
	hash, err := wt.Commit(message, &git.CommitOptions{Author: &object.Signature{Name: "Agent AI", Email: "agent@bot.local", When: time.Now()}})
	return hash.String(), err
}

func (r *Repository) Checkout(branch string, create bool) error {
	branch = strings.TrimSpace(branch)
	if branch == "" || strings.ContainsAny(branch, "..\\\x00") {
		return fmt.Errorf("invalid branch name")
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch), Create: create})
}

func (r *Repository) Restore(files []string, staged bool) error {
	for _, file := range files {
		if err := validateRelative(file); err != nil {
			return err
		}
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Restore(&git.RestoreOptions{Files: files, Staged: staged, Worktree: !staged})
}

func (r *Repository) Push(ctx context.Context, remote, branch string, setUpstream, force bool) error {
	if force {
		return fmt.Errorf("force push is not allowed")
	}
	if remote == "" {
		remote = git.DefaultRemoteName
	}
	opts := &git.PushOptions{RemoteName: remote}
	if branch != "" {
		if err := validateBranch(branch); err != nil {
			return err
		}
		ref := plumbing.NewBranchReferenceName(branch)
		dst := ref
		if setUpstream {
			dst = plumbing.NewRemoteReferenceName(remote, branch)
		}
		opts.RefSpecs = []config.RefSpec{config.RefSpec(ref.String() + ":" + dst.String())}
	}
	return r.repo.PushContext(ctx, opts)
}

func validateRelative(file string) error {
	p := path.Clean(file)
	if file == "" || p == "." || p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(file) {
		return fmt.Errorf("invalid path")
	}
	return nil
}

func validateBranch(branch string) error {
	if branch == "" || strings.ContainsAny(branch, "..\\\x00") {
		return fmt.Errorf("invalid branch name")
	}
	return nil
}

type PullRequestInput struct {
	APIURL string
	Token  string
	Title  string
	Body   string
	Head   string
	Base   string
}

func CreatePullRequest(ctx context.Context, in PullRequestInput) (map[string]any, error) {
	if in.APIURL == "" || in.Title == "" || in.Head == "" || in.Base == "" {
		return nil, fmt.Errorf("api_url, title, head and base are required")
	}
	if !strings.HasPrefix(in.APIURL, "https://") {
		return nil, fmt.Errorf("pull request API must use HTTPS")
	}
	payload, err := json.Marshal(map[string]string{"title": in.Title, "body": in.Body, "head": in.Head, "base": in.Base})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.APIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if in.Token != "" {
		req.Header.Set("Authorization", "Bearer "+in.Token)
	}
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("pull request API returned %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	result := map[string]any{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode pull request response: %w", err)
	}
	return result, nil
}
