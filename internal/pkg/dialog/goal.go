/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Goal is an execution plan for one dialog. It is persisted next to
// history.jsonl so a browser reconnect can restore its live state.
type Goal struct {
	ID        string        `json:"id"`
	DialogID  string        `json:"dialogId"`
	Goal      string        `json:"goal"`
	Status    string        `json:"status"`
	Tasks     []GoalTask    `json:"tasks"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Approval  *GoalApproval `json:"approval,omitempty"`
}

// GoalApproval is persisted while the engine waits for a browser response.
// This allows a reconnecting client to restore the confirmation dialog.
type GoalApproval struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Command   string    `json:"command,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type GoalTask struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Tools       []string `json:"tools,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	Attempts    int      `json:"attempts"`
	MaxAttempts int      `json:"maxAttempts"`
	LastTool    string   `json:"lastTool,omitempty"`
	// Arguments are retained only for the most recent successful/attempted
	// invocation. They let a declined completion approval re-run the same
	// evidence-gathering step instead of asking the model to remember it.
	Arguments  map[string]any `json:"arguments,omitempty"`
	LastResult string         `json:"lastResult,omitempty"`
	Error      string         `json:"error,omitempty"`
	StartedAt  time.Time      `json:"startedAt,omitempty"`
	FinishedAt time.Time      `json:"finishedAt,omitempty"`
	// Status is pending, running, done, failed, or skipped. A planned task is
	// skipped when the successful run did not need a matching tool operation.
	Status string `json:"status"`
}

// Goals returns the latest snapshot of every goal recorded for a dialog.
// Updates are appended so a crash cannot destroy the previous execution plan.
func (s *Store) Goals(id string) ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.goalsPath(id)
	b, err := os.ReadFile(path)
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
		var goal Goal
		if err := json.Unmarshal([]byte(line), &goal); err != nil {
			return nil, fmt.Errorf("decode dialog goal history: %w", err)
		}
		if _, exists := byID[goal.ID]; !exists {
			order = append(order, goal.ID)
		}
		byID[goal.ID] = goal
	}
	items := make([]Goal, 0, len(order))
	for _, goalID := range order {
		items = append(items, byID[goalID])
	}
	return items, nil
}

func (s *Store) goalPath(id string) string {
	parts := strings.SplitN(id, sessionSeparator, 2)
	if len(parts) == 2 && safePart(parts[0]) && safePart(parts[1]) {
		return filepath.Join(s.Root, parts[0], parts[1], "goal.json")
	}
	return filepath.Join(s.Root, filepath.Base(id)+".goal.json") // legacy callers
}

func (s *Store) goalsPath(id string) string {
	path := s.goalPath(id)
	return filepath.Join(filepath.Dir(path), "goals.jsonl")
}

// Goal returns nil when a dialog has no current tool-driven goal.
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
	var goal Goal
	if err := json.Unmarshal(b, &goal); err != nil {
		return nil, fmt.Errorf("decode dialog goal: %w", err)
	}
	if goal.Tasks == nil {
		goal.Tasks = []GoalTask{}
	}
	return &goal, nil
}

func (s *Store) SaveGoal(id string, goal Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if goal.ID == "" || goal.Goal == "" {
		return fmt.Errorf("goal id and title are required")
	}
	if goal.CreatedAt.IsZero() {
		goal.CreatedAt = time.Now().UTC()
	}
	goal.UpdatedAt = time.Now().UTC()
	if goal.Tasks == nil {
		goal.Tasks = []GoalTask{}
	}
	b, err := json.MarshalIndent(goal, "", "  ")
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
	line, err := json.Marshal(goal)
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

// DeleteGoals removes current and historical goal state for a deleted or
// cleared conversation.
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
