/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrOutsideRoot = errors.New("path is outside workspace root")

type Service struct {
	Path string
	root *os.Root
}
type FileInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDir     bool   `json:"isDir,omitempty"`
	Size      int64  `json:"size"`
	Ext       string `json:"ext"`
	UpdatedAt string `json:"updatedAt"`
	Lines     int    `json:"lines,omitempty"`
}

func NewService(rootPath string) (*Service, error) {
	if e := os.MkdirAll(rootPath, 0755); e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return nil, e
	}
	return &Service{Path: root.Name(), root: root}, nil
}
func (s *Service) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	return s.root.Close()
}

// Root exposes the already-opened filesystem root to packages that need to
// operate inside the same capability boundary (for example, gittools).
func (s *Service) Root() *os.Root { return s.root }
func clean(name string) (string, error) {
	if name == "" {
		return ".", nil
	}
	p := path.Clean(name)
	if p == ".." || len(p) > 3 && p[:3] == "../" || path.IsAbs(p) {
		return "", ErrOutsideRoot
	}
	return p, nil
}
func (s *Service) ListFiles(dir string) ([]string, error) {
	p, e := clean(dir)
	if e != nil {
		return nil, e
	}
	f, e := s.root.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(-1)
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, len(entries))
	for _, x := range entries {
		if x.Type()&os.ModeSymlink != 0 {
			continue
		}
		out = append(out, path.Join(p, x.Name()))
	}
	return out, nil
}
func (s *Service) ListFileInfo(dir string) ([]FileInfo, error) {
	p, err := clean(dir)
	if err != nil {
		return nil, err
	}
	f, err := s.root.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := path.Join(p, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ext := path.Ext(entry.Name())
		if len(ext) > 0 {
			ext = ext[1:]
		}
		out = append(out, FileInfo{ID: name, Name: entry.Name(), IsDir: entry.IsDir(), Size: info.Size(), Ext: ext, UpdatedAt: info.ModTime().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func (s *Service) ReadFile(name string) (string, error) {
	p, e := clean(name)
	if e != nil {
		return "", e
	}
	b, e := s.root.ReadFile(p)
	if errors.Is(e, fs.ErrPermission) {
		return "", ErrOutsideRoot
	}
	return string(b), e
}

// ReadFileBytes reads a file without interpreting its contents as UTF-8.
// Callers can use this for binary previews while retaining the same root
// traversal checks as text reads.
func (s *Service) ReadFileBytes(name string) ([]byte, error) {
	p, e := clean(name)
	if e != nil {
		return nil, e
	}
	b, e := s.root.ReadFile(p)
	if errors.Is(e, fs.ErrPermission) {
		return nil, ErrOutsideRoot
	}
	return b, e
}

// FileSize returns the size of a regular workspace file without reading it.
func (s *Service) FileSize(name string) (int64, error) {
	p, e := clean(name)
	if e != nil {
		return 0, e
	}
	info, e := s.root.Stat(p)
	if e != nil {
		return 0, e
	}
	return info.Size(), nil
}
func (s *Service) WriteFile(name, content string) error {
	p, e := clean(name)
	if e != nil {
		return e
	}
	if e = s.root.MkdirAll(path.Dir(p), 0755); e != nil {
		return e
	}
	e = s.root.WriteFile(p, []byte(content), 0644)
	if errors.Is(e, fs.ErrPermission) {
		return ErrOutsideRoot
	}
	return e
}
func (s *Service) RemoveFile(name string) error {
	p, err := clean(name)
	if err != nil {
		return err
	}
	err = s.root.Remove(p)
	if errors.Is(err, fs.ErrPermission) {
		return ErrOutsideRoot
	}
	return err
}

func (s *Service) MakeDir(name string) error {
	p, err := clean(name)
	if err != nil {
		return err
	}
	return s.root.MkdirAll(p, 0755)
}
func (s *Service) Move(source, destination string) error {
	a, err := clean(source)
	if err != nil {
		return err
	}
	b, err := clean(destination)
	if err != nil {
		return err
	}
	if err = s.root.MkdirAll(path.Dir(b), 0755); err != nil {
		return err
	}
	return s.root.Rename(a, b)
}
func (s *Service) Info(name string) (FileInfo, error) {
	p, err := clean(name)
	if err != nil {
		return FileInfo{}, err
	}
	f, err := s.root.Open(p)
	if err != nil {
		return FileInfo{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return FileInfo{}, err
	}
	ext := filepath.Ext(info.Name())
	return FileInfo{ID: p, Name: info.Name(), Size: info.Size(), Ext: strings.TrimPrefix(ext, "."), UpdatedAt: info.ModTime().Format(time.RFC3339)}, nil
}
func (s *Service) Replace(name, old, replacement string) error {
	content, err := s.ReadFile(name)
	if err != nil {
		return err
	}
	if strings.Count(content, old) != 1 {
		return errors.New("old_text must occur exactly once")
	}
	return s.WriteFile(name, strings.Replace(content, old, replacement, 1))
}

type Edit struct{ OldText, NewText string }

func (s *Service) EditFile(name string, edits []Edit) error {
	content, err := s.ReadFile(name)
	if err != nil {
		return err
	}
	for _, edit := range edits {
		if edit.OldText == "" || strings.Count(content, edit.OldText) != 1 {
			return fmt.Errorf("oldText must occur exactly once")
		}
		content = strings.Replace(content, edit.OldText, edit.NewText, 1)
	}
	return s.WriteFile(name, content)
}
func (s *Service) Search(pattern, dir, include string, sensitive bool) ([]map[string]any, error) {
	p, err := clean(dir)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if !sensitive {
		re, err = regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, err
		}
	}
	out := []map[string]any{}
	err = fs.WalkDir(s.root.FS(), p, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() && errors.Is(walkErr, os.ErrPermission) {
				return fs.SkipDir
			}
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		entryName := entry.Name()
		if include != "" && !matchPattern(include, entryName) {
			return nil
		}
		body, e := s.root.ReadFile(name)
		if e != nil {
			return nil
		}
		text := string(body)
		for lineNo, line := range strings.Split(text, "\n") {
			if re.MatchString(line) {
				out = append(out, map[string]any{"path": name, "line": lineNo + 1, "text": strings.Split(text, "\n")[lineNo]})
			}
		}
		return nil
	})
	return out, err
}
func matchPattern(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}
