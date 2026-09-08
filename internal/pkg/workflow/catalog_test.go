package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceCatalogJSONIncludesResourceDetails(t *testing.T) {
	catalog := ResourceCatalog{
		Servers: []ResourceServer{{
			ID: "builtin:workspace", Name: "Workspace", Prefix: "fs", Type: "builtin",
			Tools: []ResourceTool{{Name: "fs.read_file", Description: "Read a file."}},
		}},
		Agents: []AgentResources{{ID: "coder", Name: "Coder", MCPIDs: []string{"builtin:workspace"}, Tools: []string{"fs.read_file"}}},
	}
	b, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"id":"builtin:workspace"`, `"name":"Workspace"`, `"prefix":"fs"`, `"type":"builtin"`,
		`"name":"fs.read_file"`, `"description":"Read a file."`, `"id":"coder"`, `"name":"Coder"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("catalog JSON %s does not contain %s", b, want)
		}
	}
}
