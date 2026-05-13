package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Settings struct {
	Values map[string]string `json:"values"`
}

type Definition struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
	Dynamic     bool   `json:"dynamic,omitempty"`
}

const (
	TypeBool     = "bool"
	TypeInt      = "int"
	TypeFloat    = "float"
	TypeString   = "string"
	TypePathList = "path_list"
	TypeDocClass = "doc_class"
)

var settingDefinitions = []Definition{
	{
		Key:         "mine.include_hidden_operational",
		Type:        TypeBool,
		Default:     "true",
		Description: "Include allowlisted hidden operational files such as .github/SECURITY.md without enabling every hidden file.",
	},
	{
		Key:         "mine.follow_repo_symlinks",
		Type:        TypeBool,
		Default:     "true",
		Description: "Follow symlinks that resolve inside the mined root for supported text files.",
	},
	{
		Key:         "mine.include_paths",
		Type:        TypePathList,
		Description: "Comma-separated path globs to force into mining, subject to file size and UTF-8 checks.",
	},
	{
		Key:         "mine.exclude_paths",
		Type:        TypePathList,
		Description: "Comma-separated path globs to exclude from mining even when they otherwise look important.",
	},
	{
		Key:         "backup.dir",
		Type:        TypeString,
		Description: "Default destination directory for recoil backup snapshots.",
	},
	{
		Key:         "backup.max",
		Type:        TypeInt,
		Default:     "3",
		Description: "Maximum number of rotating backup snapshots to keep.",
	},
	{
		Key:         "session-evidence.enabled",
		Type:        TypeBool,
		Default:     "false",
		Description: "Enable mining session evidence captured by installed agent hooks.",
	},
	{
		Key:         "session-evidence.min-chars",
		Type:        TypeInt,
		Default:     "200",
		Description: "Minimum content length for session evidence files before ingestion selects them.",
	},
}

var dynamicDefinitions = []Definition{
	{
		Key:         "classify.override.",
		Type:        TypeDocClass,
		Description: "Assign a doc_class to paths matching the glob after this prefix, for example classify.override.docs/runbooks/**=operational.",
		Dynamic:     true,
	},
	{
		Key:         "rank.search.boost.",
		Type:        TypeFloat,
		Default:     "0",
		Description: "Add a search ranking boost for the doc_class after this prefix, for example rank.search.boost.operational=1.5.",
		Dynamic:     true,
	},
	{
		Key:         "rank.search.penalty.",
		Type:        TypeFloat,
		Default:     "0",
		Description: "Subtract a search ranking penalty for the doc_class after this prefix.",
		Dynamic:     true,
	},
	{
		Key:         "rank.wake.boost.",
		Type:        TypeFloat,
		Default:     "0",
		Description: "Add a wake ranking boost for the doc_class after this prefix.",
		Dynamic:     true,
	},
	{
		Key:         "rank.wake.penalty.",
		Type:        TypeFloat,
		Default:     "0",
		Description: "Subtract a wake ranking penalty for the doc_class after this prefix.",
		Dynamic:     true,
	},
}

var allowedDocClasses = map[string]bool{
	"agent_instructions":         true,
	"contributing":               true,
	"developers":                 true,
	"security":                   true,
	"root_readme":                true,
	"readme":                     true,
	"operational":                true,
	"changelog":                  true,
	"example":                    true,
	"fixture":                    true,
	"generated_or_public_static": true,
	"product_docs":               true,
	"package_readme":             true,
}

func ResolveSettingsPath(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) != "" {
		return filepath.Join(projectRoot, ".recoil", "config.json"), nil
	}
	dir, err := ResolveStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LoadSettings(path string) (Settings, error) {
	settings := Settings{Values: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	if settings.Values == nil {
		settings.Values = map[string]string{}
	}
	return settings, nil
}

func SaveSettings(path string, settings Settings) error {
	if settings.Values == nil {
		settings.Values = map[string]string{}
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s Settings) Get(key string) (string, bool) {
	value, ok := s.Values[strings.TrimSpace(key)]
	return value, ok
}

func (s *Settings) Set(key, value string) {
	if s.Values == nil {
		s.Values = map[string]string{}
	}
	s.Values[strings.TrimSpace(key)] = strings.TrimSpace(value)
}

func (s *Settings) Unset(key string) {
	if s.Values == nil {
		return
	}
	delete(s.Values, strings.TrimSpace(key))
}

func (s Settings) Bool(key string, def bool) bool {
	raw, ok := s.Get(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return value
}

func (s Settings) Float64(key string, def float64) float64 {
	raw, ok := s.Get(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return def
	}
	return value
}

func (s Settings) PathList(key string) []string {
	raw, ok := s.Get(key)
	if !ok {
		return nil
	}
	return SplitList(raw)
}

func SplitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.TrimPrefix(filepath.ToSlash(part), "./"))
	}
	return out
}

func Definitions() []Definition {
	defs := make([]Definition, 0, len(settingDefinitions)+len(dynamicDefinitions))
	defs = append(defs, settingDefinitions...)
	defs = append(defs, dynamicDefinitions...)
	return defs
}

func DefinitionForKey(key string) (Definition, bool) {
	key = strings.TrimSpace(key)
	for _, def := range settingDefinitions {
		if key == def.Key {
			return def, true
		}
	}
	for _, def := range dynamicDefinitions {
		if strings.HasPrefix(key, def.Key) && strings.TrimSpace(strings.TrimPrefix(key, def.Key)) != "" {
			resolved := def
			resolved.Key = key
			return resolved, true
		}
	}
	return Definition{}, false
}

func EffectiveValue(settings Settings, key string) (value, source string, ok bool) {
	key = strings.TrimSpace(key)
	if value, found := settings.Get(key); found {
		return value, "project", true
	}
	def, found := DefinitionForKey(key)
	if !found {
		return "", "", false
	}
	if def.Dynamic && def.Default == "" {
		return "", "default", true
	}
	return def.Default, "default", true
}

func EffectiveValues(settings Settings) map[string]string {
	values := map[string]string{}
	for _, def := range settingDefinitions {
		values[def.Key] = def.Default
	}
	for key, value := range settings.Values {
		values[key] = value
	}
	return values
}

func Validate(key, value string) error {
	def, ok := DefinitionForKey(key)
	if !ok {
		return fmt.Errorf("unknown config key %q", strings.TrimSpace(key))
	}
	value = strings.TrimSpace(value)
	switch def.Type {
	case TypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("config key %q expects a boolean", key)
		}
	case TypeInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("config key %q expects an integer", key)
		}
	case TypeFloat:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("config key %q expects a number", key)
		}
	case TypePathList:
		for _, item := range SplitList(value) {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("config key %q contains an empty path pattern", key)
			}
		}
	case TypeDocClass:
		if !allowedDocClasses[value] {
			return fmt.Errorf("config key %q expects a known doc_class", key)
		}
	}
	return nil
}

func (s Settings) Int(key string, def int) int {
	raw, ok := s.Get(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return value
}
