package interaction

import "testing"

func TestInferChoiceRecognizesExplicitRussianMenu(t *testing.T) {
	request, ok := InferChoice("Пожалуйста, выберите направление:\n1. Проверить доступ\n2. Проверить изоляцию профилей")
	if !ok {
		t.Fatal("expected a choice request")
	}
	if request["kind"] != "choice" {
		t.Fatalf("kind = %v, want choice", request["kind"])
	}
	options, ok := request["options"].([]map[string]string)
	if !ok || len(options) != 2 || options[1]["label"] != "Проверить изоляцию профилей" {
		t.Fatalf("unexpected options: %#v", request["options"])
	}
}

func TestInferChoiceDoesNotTurnOrdinaryListIntoPrompt(t *testing.T) {
	if _, ok := InferChoice("Project files:\n1. package.json\n2. src/main.ts"); ok {
		t.Fatal("ordinary list must not become a user choice")
	}
}
