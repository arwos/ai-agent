/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package workspace

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Entry struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"-"`
}
type Registry struct {
	mu     sync.RWMutex
	items  map[string]*Service
	opened map[string]time.Time
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]*Service{}, opened: map[string]time.Time{}}
}
func New() *Registry { return NewRegistry() }
func (r *Registry) Open(id, path string) (Entry, error) {
	if id == "" {
		return Entry{}, fmt.Errorf("workspace id is required")
	}
	s, e := NewService(path)
	if e != nil {
		return Entry{}, e
	}
	r.mu.Lock()
	old := r.items[id]
	r.items[id] = s
	openedAt := time.Now().UTC()
	r.opened[id] = openedAt
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return Entry{ID: id, Path: s.Path, OpenedAt: openedAt}, nil
}

// OpenNext opens a workspace under a generated, unused identifier.
func (r *Registry) OpenNext(path string) (Entry, error) {
	s, err := NewService(path)
	if err != nil {
		return Entry{}, err
	}
	base := strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' {
			return char
		}
		return '-'
	}, filepath.Base(s.Path))
	if base == "" || base == "." || base == "-" {
		base = "workspace"
	}
	r.mu.Lock()
	for id, existing := range r.items {
		if sameWorkspacePath(existing.Path, s.Path) {
			openedAt := r.opened[id]
			r.mu.Unlock()
			_ = s.Close()
			return Entry{ID: id, Path: existing.Path, OpenedAt: openedAt}, nil
		}
	}
	id := base
	for index := 2; r.items[id] != nil; index++ {
		id = fmt.Sprintf("%s-%d", base, index)
	}
	r.items[id] = s
	openedAt := time.Now().UTC()
	r.opened[id] = openedAt
	r.mu.Unlock()
	return Entry{ID: id, Path: s.Path, OpenedAt: openedAt}, nil
}

func sameWorkspacePath(first, second string) bool {
	first, second = filepath.Clean(first), filepath.Clean(second)
	if first == second {
		return true
	}
	// Windows paths are case-insensitive in the overwhelming majority of local
	// installations. Keeping this comparison platform-specific avoids merging
	// distinct Unix directories that differ only by case.
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return false
}
func (r *Registry) Get(id string) (*Service, error) {
	r.mu.RLock()
	s := r.items[id]
	r.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("workspace %q not found", id)
	}
	return s, nil
}

// Entry returns the current public registry descriptor for one workspace.
func (r *Registry) Entry(id string) (Entry, error) {
	r.mu.RLock()
	s := r.items[id]
	openedAt := r.opened[id]
	r.mu.RUnlock()
	if s == nil {
		return Entry{}, fmt.Errorf("workspace %q not found", id)
	}
	return Entry{ID: id, Path: s.Path, OpenedAt: openedAt}, nil
}

func (r *Registry) List() []Entry {
	r.mu.RLock()
	out := make([]Entry, 0, len(r.items))
	for id, s := range r.items {
		out = append(out, Entry{ID: id, Path: s.Path, OpenedAt: r.opened[id]})
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	s := r.items[id]
	delete(r.items, id)
	delete(r.opened, id)
	r.mu.Unlock()
	if s == nil {
		return fmt.Errorf("workspace %q not found", id)
	}
	return s.Close()
}
