/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package goal persists execution plans associated with dialogs.
package goal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sessionSeparator = "\x00"

type Store struct {
	Root string
	mu   sync.Mutex
}

type Goal struct {
	ID              string        `json:"id"`
	DialogID        string        `json:"dialogId"`
	Goal            string        `json:"goal"`
	Request         string        `json:"request,omitempty"`
	Status          string        `json:"status"`
	Tasks           []GoalTask    `json:"tasks"`
	WorkflowID      string        `json:"workflowId,omitempty"`
	WorkflowVersion int           `json:"workflowVersion,omitempty"`
	WorkflowHash    string        `json:"workflowHash,omitempty"`
	CurrentTaskID   string        `json:"currentTaskId,omitempty"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
	Approval        *GoalApproval `json:"approval,omitempty"`
}

type GoalApproval struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Command   string    `json:"command,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type GoalTask struct {
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	Tools            []string        `json:"tools,omitempty"`
	Skills           []string        `json:"skills,omitempty"`
	DependsOn        []string        `json:"dependsOn,omitempty"`
	Attempts         int             `json:"attempts"`
	MaxAttempts      int             `json:"maxAttempts"`
	LastTool         string          `json:"lastTool,omitempty"`
	Arguments        map[string]any  `json:"arguments,omitempty"`
	LastResult       string          `json:"lastResult,omitempty"`
	Error            string          `json:"error,omitempty"`
	StartedAt        time.Time       `json:"startedAt,omitempty"`
	FinishedAt       time.Time       `json:"finishedAt,omitempty"`
	Status           string          `json:"status"`
	WorkflowStepID   string          `json:"workflowStepId,omitempty"`
	WorkflowSnapshot json.RawMessage `json:"workflowSnapshot,omitempty"`
	// Executor is retained for old persisted goals. New workflow runs use ExecutorAgentID.
	Executor         string         `json:"executor,omitempty"`
	ExecutorAgentID  string         `json:"executorAgentId,omitempty"`
	ReviewerAgentID  string         `json:"reviewerAgentId,omitempty"`
	ChildDialogID    string         `json:"childDialogId,omitempty"`
	ReviewerDialogID string         `json:"reviewerDialogId,omitempty"`
	ChildAgentID     string         `json:"childAgentId,omitempty"`
	RunTokenHash     string         `json:"runTokenHash,omitempty"`
	ExpectedResult   string         `json:"expectedResult,omitempty"`
	ResultMetadata   map[string]any `json:"resultMetadata,omitempty"`
	Acceptance       string         `json:"acceptance,omitempty"`
	ReviewVerdict    string         `json:"reviewVerdict,omitempty"`
	ReviewSummary    string         `json:"reviewSummary,omitempty"`
	ReviewEvidence   []string       `json:"reviewEvidence,omitempty"`
	ReviewFeedback   string         `json:"reviewFeedback,omitempty"`
	History          []TaskAttempt  `json:"history,omitempty"`
}

type TaskAttempt struct {
	Attempt        int      `json:"attempt"`
	BuilderResult  string   `json:"builderResult,omitempty"`
	ReviewVerdict  string   `json:"reviewVerdict,omitempty"`
	ReviewSummary  string   `json:"reviewSummary,omitempty"`
	ReviewEvidence []string `json:"reviewEvidence,omitempty"`
	ReviewFeedback string   `json:"reviewFeedback,omitempty"`
}

func NewStore(root string) *Store { return &Store{Root: root} }

func (s *Store) goalPath(id string) string {
	parts := strings.SplitN(id, sessionSeparator, 2)
	if len(parts) == 2 && safePart(parts[0]) && safePart(parts[1]) {
		return filepath.Join(s.Root, parts[0], parts[1], "goal.json")
	}
	return filepath.Join(s.Root, filepath.Base(id)+".goal.json")
}

func (s *Store) goalsPath(id string) string {
	return filepath.Join(filepath.Dir(s.goalPath(id)), "goals.jsonl")
}

func safePart(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func (s *Store) Goals(id string) ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.goalsPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return []Goal{}, nil
	}
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Goal)
	order := make([]string, 0)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var item Goal
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("decode dialog goal history: %w", err)
		}
		if _, exists := byID[item.ID]; !exists {
			order = append(order, item.ID)
		}
		byID[item.ID] = item
	}
	items := make([]Goal, 0, len(order))
	for _, id := range order {
		items = append(items, byID[id])
	}
	return items, nil
}

func (s *Store) Goal(id string) (*Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.goalPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var item Goal
	if err := json.Unmarshal(b, &item); err != nil {
		return nil, fmt.Errorf("decode dialog goal: %w", err)
	}
	if item.Tasks == nil {
		item.Tasks = []GoalTask{}
	}
	return &item, nil
}

func (s *Store) SaveGoal(id string, item Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" || item.Goal == "" {
		return fmt.Errorf("goal id and title are required")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	item.UpdatedAt = time.Now().UTC()
	if item.Tasks == nil {
		item.Tasks = []GoalTask{}
	}
	b, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	path := s.goalPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".goal-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(0644); err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	line, err := json.Marshal(item)
	if err != nil {
		return err
	}
	history, err := os.OpenFile(s.goalsPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644) //nolint:gosec // validated input or bounded archive is required here
	if err != nil {
		return err
	}
	_, writeErr := history.Write(append(line, '\n'))
	closeErr := history.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (s *Store) DeleteGoal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.goalPath(id)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(filepath.Dir(path))
	return nil
}

func (s *Store) DeleteGoals(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.goalPath(id)
	for _, candidate := range []string{path, s.goalsPath(id)} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	_ = os.Remove(filepath.Dir(path))
	return nil
}
