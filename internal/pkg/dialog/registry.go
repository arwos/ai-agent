/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"fmt"
	"sync"
)

type Session struct {
	ID    string
	Store *Store
}
type Registry struct {
	store *Store
	pool  sync.Pool
}

func NewStore(root string, limits ...int) *Store {
	limit := 100
	if len(limits) > 0 && limits[0] > 0 {
		limit = limits[0]
	}
	return &Store{Root: root, HistoryLimit: limit}
}
func NewRegistry(store *Store) *Registry {
	r := &Registry{store: store}
	r.pool.New = func() any { return &Session{} }
	return r
}
func (r *Registry) Acquire(id string) *Session {
	s := r.pool.Get().(*Session)
	s.ID = id
	s.Store = r.store
	return s
}
func (r *Registry) Release(s *Session) {
	if s == nil {
		return
	}
	s.ID = ""
	s.Store = nil
	r.pool.Put(s)
}
func (s *Session) Append(m Message) error {
	if s.Store == nil || s.ID == "" {
		return fmt.Errorf("dialog session is not initialized")
	}
	return s.Store.Append(s.ID, m)
}
func (s *Session) History(limit int) ([]Message, error) {
	if s.Store == nil || s.ID == "" {
		return nil, fmt.Errorf("dialog session is not initialized")
	}
	return s.Store.History(s.ID, limit)
}
