package workflow

import "testing"

func TestValidateWorkflowSchema(t *testing.T) {
	d := Definition{ID: "release", Name: "Release", Enabled: true, Steps: []Step{{ID: "build", Title: "Build", Executor: "self", Retry: Retry{MaxAttempts: 1}, Inputs: []Input{{Name: "request", Ref: "user_request"}}}}}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	d.Steps[0].Executor = "shell"
	if err := Validate(d); err == nil {
		t.Fatal("expected unsupported executor error")
	}
}

func TestServiceVersioningAndProfileIsolation(t *testing.T) {
	s := New(t.TempDir())
	d := Definition{ID: "demo", Name: "Demo", Enabled: true, Steps: []Step{{ID: "one", Title: "One", Executor: "self"}}}
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
