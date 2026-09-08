package docker

import (
	"context"
	"os/exec"
	"runtime"
	"slices"
	"testing"
)

func TestRun(t *testing.T) {
	var gotName string
	var gotArgs []string
	service := New()
	service.commandContext = func(_ context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.Command("printf", "container-id\n")
	}

	id, err := service.Run(context.Background(), "qdrant", "qdrant/qdrant", "-p", "6333:6333")
	if err != nil || id != "container-id" {
		t.Fatalf("Run() = %q, %v", id, err)
	}
	if gotName != "docker" || !slices.Equal(gotArgs, []string{"run", "-d", "--name", "qdrant", "-p", "6333:6333", "qdrant/qdrant"}) {
		t.Fatalf("command = %s %v", gotName, gotArgs)
	}
}

func TestContainerCommandValidation(t *testing.T) {
	if err := New().Stop(context.Background(), " "); err == nil {
		t.Fatal("Stop() accepted an empty name")
	}
	if _, err := New().Run(context.Background(), "", "image"); err == nil {
		t.Fatal("Run() accepted an empty name")
	}
}

func TestInstallCommand(t *testing.T) {
	tests := []struct {
		goos string
		want []string
	}{
		{"linux", []string{"sh", "-c", "curl -fsSL https://get.docker.com/rootless | SKIP_IPTABLES=1 sh"}},
		{"darwin", []string{"brew", "install", "--cask", "docker"}},
		{"windows", []string{"winget", "install", "--exact", "--id", "Docker.DockerDesktop"}},
	}
	for _, test := range tests {
		got, err := InstallCommand(test.goos)
		if err != nil || !slices.Equal(got, test.want) {
			t.Errorf("InstallCommand(%q) = %v, %v", test.goos, got, err)
		}
	}
	if _, err := InstallCommand("plan9"); err == nil {
		t.Fatal("InstallCommand(plan9) did not fail")
	}
}

func TestInstallChecksUIDMap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uidmap is required only for Linux rootless Docker")
	}
	service := New()
	service.lookPath = func(file string) (string, error) {
		if file == "newuidmap" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	err := service.Install(context.Background())
	if err == nil || err.Error() == "" {
		t.Fatal("Install() did not report a missing uidmap dependency")
	}
}
