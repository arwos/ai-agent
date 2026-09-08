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
