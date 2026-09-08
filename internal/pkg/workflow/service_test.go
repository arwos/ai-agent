package workflow

import "testing"

func TestValidateWorkflowSchema(t *testing.T) {
	d := Definition{ID: "release", Name: "Release", Enabled: true, Steps: []Step{{ID: "build", Title: "Build", AgentID: "builder", Acceptance: "The build succeeds.", Retry: Retry{MaxAttempts: 1}}}}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	d.Steps[0].Acceptance = ""
	if err := Validate(d); err == nil {
		t.Fatal("expected missing acceptance error")
	}
}

func TestValidateAllowsDisabledEmptyDraft(t *testing.T) {
	d := Definition{ID: "draft", Name: "Draft", Enabled: false}
	if err := Validate(d); err != nil {
		t.Fatalf("disabled empty draft rejected: %v", err)
	}
	d.Enabled = true
	if err := Validate(d); err == nil {
		t.Fatal("enabled empty workflow accepted")
	}
}

func TestValidateAllowsCatalogResourceNames(t *testing.T) {
	d := Definition{ID: "draft", Name: "Draft", Steps: []Step{{
		ID:             "initialize",
		Title:          "Initialize Go module",
		AgentID:        "agent-builder",
		Acceptance:     "The module files exist.",
		Retry:          Retry{MaxAttempts: 1},
		RequiredTools:  []string{"git.init", "fs.mkdir", "fs.write_file"},
		RequiredSkills: []string{"skill-group-golang"},
	}}}
	if err := Validate(d); err != nil {
		t.Fatalf("catalog resource names rejected: %v", err)
	}
}

func TestValidateWorkflowRejectsUnsafeStepContracts(t *testing.T) {
	valid := func() Definition {
		return Definition{ID: "release", Name: "Release", Enabled: true, Steps: []Step{{
			ID: "build", Title: "Build", AgentID: "builder", Acceptance: "A readable result is produced.", Retry: Retry{MaxAttempts: 1},
			RequiredTools: []string{"fs.read_file"},
		}}}
	}
	cases := []struct {
		name string
		edit func(*Definition)
	}{
		{"agent ID required", func(d *Definition) { d.Steps[0].AgentID = "" }},
		{"substep IDs unique", func(d *Definition) {
			d.Steps[0].Substeps = []Substep{{ID: "check", Title: "Check"}, {ID: "check", Title: "Again"}}
		}},
		{"unsafe capability", func(d *Definition) { d.Steps[0].RequiredTools = []string{"../write"} }},
		{"acceptance required", func(d *Definition) { d.Steps[0].Acceptance = "" }},
		{"unknown dependency", func(d *Definition) { d.Steps[0].Dependencies = []string{"missing"} }},
		{"self dependency", func(d *Definition) { d.Steps[0].Dependencies = []string{"build"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.edit(&d)
			if err := Validate(d); err == nil {
				t.Fatal("invalid workflow contract accepted")
			}
		})
	}
}

func TestServiceVersioningAndProfileIsolation(t *testing.T) {
	s := New(t.TempDir())
	d := Definition{ID: "demo", Name: "Demo", Enabled: true, Steps: []Step{{ID: "one", Title: "One", AgentID: "builder", Acceptance: "Done.", Retry: Retry{MaxAttempts: 1}}}}
	saved, err := s.Save("profile-a", d)
	if err != nil || saved.Version != 1 {
		t.Fatalf("first save = %#v, %v", saved, err)
	}
	saved.Name = "Updated"
	saved, err = s.Save("profile-a", saved)
	if err != nil || saved.Version != 2 {
		t.Fatalf("second save = %#v, %v", saved, err)
	}
	if _, err = s.Get("profile-b", "demo"); err == nil {
		t.Fatal("workflow leaked across profiles")
	}
	if _, err = s.Get("profile-a", "../demo"); err == nil {
		t.Fatal("path traversal workflow id accepted")
	}
}

func TestDecodeStepAcceptsToolLists(t *testing.T) {
	var step Step
	if err := decodeStep(map[string]any{
		"title":           "Use every available tool",
		"required_tools":  []string{"fs.read_file", "go.go_search"},
		"required_skills": []string{"golang"},
	}, &step); err != nil {
		t.Fatal(err)
	}
	if len(step.RequiredTools) != 2 || step.RequiredTools[1] != "go.go_search" {
		t.Fatalf("required tools were not preserved: %#v", step.RequiredTools)
	}
	if len(step.RequiredSkills) != 1 || step.RequiredSkills[0] != "golang" {
		t.Fatalf("required skills were not preserved: %#v", step.RequiredSkills)
	}
}
