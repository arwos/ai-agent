package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	maxSteps      = 32
	maxSubsteps   = 32
	maxText       = 8192
	maxIdentifier = 128
)

type Definition struct {
	Version     int    `json:"version" yaml:"version"`
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Steps       []Step `json:"steps" yaml:"steps"`
}

type Step struct {
	ID             string    `json:"id" yaml:"id"`
	Title          string    `json:"title" yaml:"title"`
	Description    string    `json:"description,omitempty" yaml:"description,omitempty"`
	Substeps       []Substep `json:"substeps,omitempty" yaml:"substeps,omitempty"`
	Acceptance     string    `json:"acceptance" yaml:"acceptance"`
	Dependencies   []string  `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	RequiredTools  []string  `json:"required_tools,omitempty" yaml:"required_tools,omitempty"`
	RequiredSkills []string  `json:"required_skills,omitempty" yaml:"required_skills,omitempty"`
	AgentID        string    `json:"agent_id" yaml:"agent_id"`
	Retry          Retry     `json:"retry,omitempty" yaml:"retry,omitempty"`
}

type Substep struct {
	ID          string `yaml:"id" json:"id"`
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}
type Retry struct {
	MaxAttempts int `yaml:"max_attempts" json:"maxAttempts"`
}

type Service struct {
	Root string
	mu   sync.RWMutex
}

type Artifact struct {
	ID        string `json:"id"`
	ProfileID string `json:"-"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

func (s *Service) artifactPath(profile, id string) (string, error) {
	if !safePart(profile) || !safePart(id) {
		return "", fmt.Errorf("invalid profile or artifact id")
	}
	return filepath.Join(s.Root, profile, "artifacts", id), nil
}

func safeArtifactName(name string) bool {
	clean := filepath.ToSlash(filepath.Clean(name))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}

func (s *Service) CreateArtifact(profile, name string, content []byte) (Artifact, error) {
	if !safePart(profile) || !safeArtifactName(name) || len(content) > 100<<20 {
		return Artifact{}, fmt.Errorf("invalid artifact")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("artifact-%x", sha256.Sum256(append([]byte(name), content...)))
	path, err := s.artifactPath(profile, id)
	if err != nil {
		return Artifact{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Artifact{}, err
	}
	if err = os.WriteFile(path, content, 0644); err != nil {
		return Artifact{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{ID: id, ProfileID: profile, Name: name, Size: info.Size(), CreatedAt: info.ModTime().UTC().Format(time.RFC3339)}, nil
}

func (s *Service) GetArtifact(profile, id string) (Artifact, error) {
	path, err := s.artifactPath(profile, id)
	if err != nil {
		return Artifact{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{ID: id, ProfileID: profile, Name: id, Size: info.Size(), CreatedAt: info.ModTime().UTC().Format(time.RFC3339)}, nil
}

func (s *Service) ReadArtifact(profile, id string) ([]byte, error) {
	path, err := s.artifactPath(profile, id)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 100<<20 {
		return nil, fmt.Errorf("artifact exceeds read limit")
	}
	return os.ReadFile(path)
}

func New(root string) *Service { return &Service{Root: filepath.Join(root, "workflow")} }

func (s *Service) path(profile, id string) (string, error) {
	if !safePart(profile) || !safePart(id) {
		return "", fmt.Errorf("invalid profile or workflow id")
	}
	return filepath.Join(s.Root, profile, id+".yaml"), nil
}
func safePart(v string) bool {
	return v != "" && len(v) <= maxIdentifier && v != "." && v != ".." && filepath.Base(v) == v && !strings.ContainsAny(v, `/\\`)
}

func Validate(d Definition) error {
	if !safePart(d.ID) || strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("workflow id and name are required")
	}
	if len(d.Name) > maxText || len(d.Description) > maxText || len(d.Steps) > maxSteps || (d.Enabled && len(d.Steps) == 0) {
		return fmt.Errorf("invalid workflow size")
	}
	seenSteps := map[string]bool{}
	for _, step := range d.Steps {
		if !safePart(step.ID) || seenSteps[step.ID] {
			return fmt.Errorf("workflow steps require unique ids")
		}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("workflow step %q requires a title", step.ID)
		}
		seenSteps[step.ID] = true
		if len(step.Title) > maxText || len(step.Description) > maxText || len(step.Acceptance) > maxText || strings.TrimSpace(step.Acceptance) == "" {
			return fmt.Errorf("workflow step %q requires acceptance criteria", step.ID)
		}
		if !safePart(step.AgentID) {
			return fmt.Errorf("workflow step %q requires a valid agent_id", step.ID)
		}
		if step.Retry.MaxAttempts < 1 || step.Retry.MaxAttempts > 10 {
			return fmt.Errorf("workflow step %q requires max_attempts from 1 to 10", step.ID)
		}
		if len(step.Substeps) > maxSubsteps {
			return fmt.Errorf("workflow step %q exceeds collection limits", step.ID)
		}
		if len(step.Dependencies) > maxSteps {
			return fmt.Errorf("workflow step %q has too many dependencies", step.ID)
		}
		seenDependencies := map[string]bool{}
		for _, dependency := range step.Dependencies {
			if !safePart(dependency) || seenDependencies[dependency] || dependency == step.ID {
				return fmt.Errorf("workflow step %q has invalid dependencies", step.ID)
			}
			seenDependencies[dependency] = true
		}
		seenSubsteps := map[string]bool{}
		for _, substep := range step.Substeps {
			if !safePart(substep.ID) || seenSubsteps[substep.ID] || strings.TrimSpace(substep.Title) == "" || len(substep.Title) > maxText || len(substep.Description) > maxText {
				return fmt.Errorf("invalid substep in workflow step %q", step.ID)
			}
			seenSubsteps[substep.ID] = true
		}
		for _, tool := range step.RequiredTools {
			if !safeCapability(tool) {
				return fmt.Errorf("workflow step %q has invalid required tool %q", step.ID, tool)
			}
		}
		for _, group := range step.RequiredSkills {
			if !safeCapability(group) {
				return fmt.Errorf("workflow step %q has invalid skill group %q", step.ID, group)
			}
		}
	}
	stepOrder := make(map[string]int, len(d.Steps))
	for index, step := range d.Steps {
		stepOrder[step.ID] = index
	}
	for index, step := range d.Steps {
		for _, dependency := range step.Dependencies {
			dependencyIndex, exists := stepOrder[dependency]
			if !exists {
				return fmt.Errorf("workflow step %q depends on unknown step %q", step.ID, dependency)
			}
			if dependencyIndex >= index {
				return fmt.Errorf("workflow step %q must depend only on preceding steps", step.ID)
			}
		}
	}
	return nil
}

func safeCapability(v string) bool {
	return v != "" && len(v) <= maxIdentifier && !strings.ContainsAny(v, "/\\\t\n\r ")
}

func (s *Service) List(profile string) ([]Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.Root, profile)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Definition{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Definition, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		d, err := s.read(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *Service) Get(profile, id string) (Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.path(profile, id)
	if err != nil {
		return Definition{}, err
	}
	return s.read(p)
}
func (s *Service) read(path string) (Definition, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	var d Definition
	if err = yaml.Unmarshal(b, &d); err != nil {
		return d, err
	}
	return d, nil
}
func (s *Service) Save(profile string, d Definition) (Definition, error) {
	if err := Validate(d); err != nil {
		return d, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(profile, d.ID)
	if err != nil {
		return d, err
	}
	if old, e := s.read(p); e == nil {
		d.Version = old.Version + 1
	} else if d.Version < 1 {
		d.Version = 1
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return d, err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return d, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".workflow-*.tmp")
	if err != nil {
		return d, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return d, err
	}
	return d, os.Rename(name, p)
}
func (s *Service) Delete(profile, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(profile, id)
	if err != nil {
		return err
	}
	if err = os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (s *Service) Import(profile, content string) (Definition, error) {
	var d Definition
	if err := yaml.Unmarshal([]byte(content), &d); err != nil {
		return d, err
	}
	d.Version = 0
	// Older workflow files remain editable so users can add the now-required
	// builder agent and acceptance criteria. Do not persist an invalid import.
	if err := Validate(d); err != nil {
		return d, nil
	}
	return s.Save(profile, d)
}
func (s *Service) Export(profile, id string) (string, error) {
	d, err := s.Get(profile, id)
	if err != nil {
		return "", err
	}
	b, err := yaml.Marshal(d)
	return string(b), err
}
func Hash(d Definition) (string, error) {
	b, err := yaml.Marshal(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
