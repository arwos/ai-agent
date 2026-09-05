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

	"gopkg.in/yaml.v3"
)

const (
	maxSteps = 32
	maxText  = 8192
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
	Inputs         []Input   `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Outputs        []string  `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Validation     string    `json:"validation,omitempty" yaml:"validation,omitempty"`
	RequiredTools  []string  `json:"required_tools,omitempty" yaml:"required_tools,omitempty"`
	RequiredSkills []string  `json:"required_skills,omitempty" yaml:"required_skills,omitempty"`
	Executor       string    `json:"executor" yaml:"executor"`
	Retry          Retry     `json:"retry,omitempty" yaml:"retry,omitempty"`
}

type Substep struct {
	ID          string `yaml:"id" json:"id"`
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}
type Input struct {
	Name    string `yaml:"name" json:"name"`
	Ref     string `yaml:"ref" json:"ref"`
	Literal string `yaml:"literal,omitempty" json:"literal,omitempty"`
}
type Retry struct {
	MaxAttempts int `yaml:"max_attempts" json:"maxAttempts"`
}

type Service struct {
	Root string
	mu   sync.RWMutex
}

func New(root string) *Service { return &Service{Root: filepath.Join(root, "workflow")} }

func (s *Service) path(profile, id string) (string, error) {
	if !safePart(profile) || !safePart(id) {
		return "", fmt.Errorf("invalid profile or workflow id")
	}
	return filepath.Join(s.Root, profile, id+".yaml"), nil
}
func safePart(v string) bool {
	return v != "" && v != "." && v != ".." && filepath.Base(v) == v && !strings.ContainsAny(v, `/\\`)
}

func Validate(d Definition) error {
	if !safePart(d.ID) || strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("workflow id and name are required")
	}
	if len(d.Name) > maxText || len(d.Description) > maxText || len(d.Steps) == 0 || len(d.Steps) > maxSteps {
		return fmt.Errorf("invalid workflow size")
	}
	seen := map[string]bool{}
	for _, step := range d.Steps {
		if !safePart(step.ID) || seen[step.ID] || strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("workflow steps require unique ids and titles")
		}
		seen[step.ID] = true
		if len(step.Description) > maxText || len(step.Validation) > maxText || step.Executor == "" {
			return fmt.Errorf("invalid workflow step %q", step.ID)
		}
		if step.Executor != "self" && step.Executor != "agent_id" && step.Executor != "auto" {
			return fmt.Errorf("unsupported executor %q", step.Executor)
		}
		if step.Retry.MaxAttempts == 0 {
			step.Retry.MaxAttempts = 1
		}
		if step.Retry.MaxAttempts < 1 || step.Retry.MaxAttempts > 10 {
			return fmt.Errorf("invalid retry limit")
		}
		for _, in := range step.Inputs {
			if in.Ref != "user_request" && in.Ref != "previous_result" && in.Ref != "artifact_id" && in.Ref != "workspace_path" && in.Ref != "literal" {
				return fmt.Errorf("unsupported input reference %q", in.Ref)
			}
		}
	}
	return nil
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
	if err = Validate(d); err != nil {
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
