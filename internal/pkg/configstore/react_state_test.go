package configstore

import (
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func TestProfileScopedReactState(t *testing.T) {
	s := New(testORM(t))
	profiles, active, err := s.Profiles()
	if err != nil || active != DefaultProfileID || len(profiles) != 1 {
		t.Fatalf("profiles=%#v active=%q err=%v", profiles, active, err)
	}
	if _, err = s.AgentUpsert(models.Agent{ID: "agent-1", ProfileID: active, Name: "Writer"}); err != nil {
		t.Fatal(err)
	}
	if agents, err := s.Agents(active); err != nil || len(agents) != 1 || agents[0].Name != "Writer" {
		t.Fatalf("agents=%#v err=%v", agents, err)
	}
}
