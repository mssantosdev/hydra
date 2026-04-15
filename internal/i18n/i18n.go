package i18n

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var translationsFS embed.FS

// Translator handles internationalization
type Translator struct {
	language string
	strings  map[string]string
}

// New creates a new translator for the given language
func New(language string) *Translator {
	t := &Translator{
		language: language,
		strings:  make(map[string]string),
	}

	// Load translations
	t.load()

	return t
}

// load loads translation strings from embedded files
func (t *Translator) load() {
	filename := fmt.Sprintf("%s.yaml", t.language)

	data, err := translationsFS.ReadFile(filename)
	if err != nil {
		// Fall back to English if translation file not found
		if t.language != "en-US" {
			t.language = "en-US"
			data, _ = translationsFS.ReadFile("en-US.yaml")
		}
	}

	if data != nil {
		var translations map[string]string
		if err := yaml.Unmarshal(data, &translations); err == nil {
			t.strings = translations
		}
	}
}

// T translates a key to the current language
func (t *Translator) T(key string) string {
	if str, ok := t.strings[key]; ok {
		return str
	}
	// Return key if translation not found
	return key
}

// Tf translates a key with formatting
func (t *Translator) Tf(key string, args ...interface{}) string {
	str := t.T(key)
	return fmt.Sprintf(str, args...)
}

// SetLanguage changes the language and reloads
func (t *Translator) SetLanguage(language string) {
	t.language = language
	t.load()
}

// GetLanguage returns current language
func (t *Translator) GetLanguage() string {
	return t.language
}

// Default translator instance
var defaultTranslator *Translator

// Init initializes the default translator
func Init(language string) {
	defaultTranslator = New(language)
}

// T translates using default translator
func T(key string) string {
	if defaultTranslator == nil {
		return key
	}
	return defaultTranslator.T(key)
}

// Tf translates with formatting using default translator
func Tf(key string, args ...interface{}) string {
	if defaultTranslator == nil {
		return fmt.Sprintf(key, args...)
	}
	return defaultTranslator.Tf(key, args...)
}

// SetLanguage sets language on default translator
func SetLanguage(language string) {
	if defaultTranslator != nil {
		defaultTranslator.SetLanguage(language)
	}
}
