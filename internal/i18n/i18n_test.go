package i18n

import (
	"testing"
)

func TestNew(t *testing.T) {
	tr := New("en-US")

	if tr == nil {
		t.Fatal("Translator should not be nil")
	}

	if tr.language != "en-US" {
		t.Errorf("Expected language en-US, got %s", tr.language)
	}
}

func TestT(t *testing.T) {
	tr := New("en-US")

	// Test existing key
	result := tr.T("app.name")
	if result != "Hydra" {
		t.Errorf("Expected 'Hydra', got '%s'", result)
	}

	// Test non-existing key (should return key)
	result = tr.T("nonexistent.key")
	if result != "nonexistent.key" {
		t.Errorf("Expected 'nonexistent.key', got '%s'", result)
	}
}

func TestTf(t *testing.T) {
	tr := New("en-US")

	// Test with formatting (if we had such translations)
	result := tr.Tf("app.name")
	if result != "Hydra" {
		t.Errorf("Expected 'Hydra', got '%s'", result)
	}
}

func TestSetLanguage(t *testing.T) {
	tr := New("en-US")

	if tr.GetLanguage() != "en-US" {
		t.Errorf("Expected language en-US, got %s", tr.GetLanguage())
	}

	tr.SetLanguage("pt-BR")

	if tr.GetLanguage() != "pt-BR" {
		t.Errorf("Expected language pt-BR after set, got %s", tr.GetLanguage())
	}
}

func TestDefaultTranslator(t *testing.T) {
	// Before init, should return key
	result := T("app.name")
	if result != "app.name" {
		t.Errorf("Before init, should return key, got '%s'", result)
	}

	// Initialize
	Init("en-US")

	// After init
	result = T("app.name")
	if result != "Hydra" {
		t.Errorf("After init, expected 'Hydra', got '%s'", result)
	}

	// Test SetLanguage on default
	SetLanguage("pt-BR")
	if defaultTranslator.GetLanguage() != "pt-BR" {
		t.Errorf("Expected language pt-BR, got %s", defaultTranslator.GetLanguage())
	}
}

func TestPortugueseTranslations(t *testing.T) {
	tr := New("pt-BR")

	result := tr.T("app.description")
	if result != "Gerenciador de worktrees Git" {
		t.Errorf("Expected Portuguese translation, got '%s'", result)
	}

	result = tr.T("cmd.clone")
	if result != "Clonar novo repositório e configurar worktrees" {
		t.Errorf("Expected Portuguese clone command, got '%s'", result)
	}
}

func TestFallbackToEnglish(t *testing.T) {
	// Create translator with invalid language
	tr := New("invalid-lang")

	// Should fallback to English
	if tr.language != "en-US" {
		t.Errorf("Should fallback to en-US, got %s", tr.language)
	}

	result := tr.T("app.name")
	if result != "Hydra" {
		t.Errorf("Expected 'Hydra' after fallback, got '%s'", result)
	}
}
