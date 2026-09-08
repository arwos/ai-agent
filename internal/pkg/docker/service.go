/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package docker provides a small Docker CLI wrapper used by local services.
package docker

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type commandFunc func(context.Context, string, ...string) *exec.Cmd
type lookPathFunc func(string) (string, error)

type Service struct {
	commandContext commandFunc
	lookPath       lookPathFunc
}

func New() *Service {
	return &Service{commandContext: exec.CommandContext, lookPath: exec.LookPath}
}

// Installed reports whether the Docker CLI is available on PATH.
func (s *Service) Installed(ctx context.Context) bool {
	cmd := s.commandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run() == nil
}

// Run starts a detached container and returns its ID.
func (s *Service) Run(ctx context.Context, name, image string, args ...string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("container name is required")
	}
	if strings.TrimSpace(image) == "" {
		return "", fmt.Errorf("container image is required")
	}
	commandArgs := []string{"run", "-d", "--name", name}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, image)
	output, err := s.commandContext(ctx, "docker", commandArgs...).Output()
	if err != nil {
		return "", fmt.Errorf("run Docker container %q: %w", name, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *Service) Stop(ctx context.Context, name string) error {
	return s.containerCommand(ctx, "stop", name)
}

func (s *Service) Remove(ctx context.Context, name string) error {
	return s.containerCommand(ctx, "rm", name)
}

func (s *Service) containerCommand(ctx context.Context, operation, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("container name is required")
	}
	if err := s.commandContext(ctx, "docker", operation, name).Run(); err != nil {
		return fmt.Errorf("Docker %s container %q: %w", operation, name, err)
	}
	return nil
}

// Install installs Docker using the platform's standard package installer.
func (s *Service) Install(ctx context.Context) error {
	if runtime.GOOS == "linux" {
		for _, binary := range []string{"newuidmap", "newgidmap"} {
			if _, err := s.lookPath(binary); err != nil {
				return fmt.Errorf("Docker rootless installation requires %s; install the uidmap package first (for example: sudo apt-get install uidmap or sudo dnf install shadow-utils)", binary)
			}
		}
	}
	args, err := InstallCommand(runtime.GOOS)
	if err != nil {
		return err
	}
	if err := s.commandContext(ctx, args[0], args[1:]...).Run(); err != nil {
		return fmt.Errorf("install Docker: %w", err)
	}
	return nil
}

// InstallCommand returns the command used to install Docker on goos.
func InstallCommand(goos string) ([]string, error) {
	switch goos {
	case "linux":
		// Install Docker CE in rootless mode without configuring iptables.
		return []string{"sh", "-c", "curl -fsSL https://get.docker.com/rootless | SKIP_IPTABLES=1 sh"}, nil
	case "darwin":
		return []string{"brew", "install", "--cask", "docker"}, nil
	case "windows":
		return []string{"winget", "install", "--exact", "--id", "Docker.DockerDesktop"}, nil
	default:
		return nil, fmt.Errorf("Docker installation is not supported on %s", goos)
	}
}
