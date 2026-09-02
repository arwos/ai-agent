/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package gittools

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// Repository opens Git metadata and the worktree from one already-opened
// os.Root. No absolute-path filesystem is handed to go-git.
type Repository struct {
	root     *os.Root
	worktree billy.Filesystem
	repo     *git.Repository
}

func Open(root *os.Root) (*Repository, error) {
	if root == nil {
		return nil, errors.New("root is required")
	}
	worktree, err := osfs.FromRoot(root)
	if err != nil {
		return nil, err
	}
	dot, err := worktree.Chroot(".git")
	if err != nil {
		return nil, fmt.Errorf("open .git: %w", err)
	}
	if _, err = dot.Stat(""); err != nil {
		return nil, err
	}
	storage := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(dot, dot), cache.NewObjectLRUDefault())
	repository, err := git.Open(storage, worktree)
	if err != nil {
		_ = storage.Close()
		return nil, err
	}
	return &Repository{root: root, worktree: worktree, repo: repository}, nil
}

// Init initializes a repository entirely below the supplied os.Root.
func Init(root *os.Root) (*Repository, error) {
	if root == nil {
		return nil, errors.New("root is required")
	}
	if _, err := root.Stat(".git"); err == nil {
		return nil, fmt.Errorf("git repository already exists")
	}
	if err := root.Mkdir(".git", 0755); err != nil {
		return nil, fmt.Errorf("create .git: %w", err)
	}
	worktree, err := osfs.FromRoot(root)
	if err != nil {
		return nil, err
	}
	dot, err := worktree.Chroot(".git")
	if err != nil {
		return nil, fmt.Errorf("open .git: %w", err)
	}
	storage := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(dot, dot), cache.NewObjectLRUDefault())
	repository, err := git.Init(storage, git.WithWorkTree(worktree))
	if err != nil {
		_ = storage.Close()
		return nil, err
	}
	return &Repository{root: root, worktree: worktree, repo: repository}, nil
}

func (r *Repository) Git() *git.Repository { return r.repo }
func (r *Repository) Close() error {
	if r.repo == nil {
		return nil
	}
	return r.repo.Close()
}
