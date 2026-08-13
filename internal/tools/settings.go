package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SettingsInput is the input schema for the settings tool.
type SettingsInput struct {
	Operation string `json:"operation" jsonschema:"Operation to perform: get, set, or unset"`
	Key       string `json:"key,omitempty" jsonschema:"Setting key (e.g. shellPath, shellCommandPrefix)"`
	Value     any    `json:"value,omitempty" jsonschema:"Value to set (only for set)"`
}

// SettingDef describes one known setting key.
type SettingDef struct {
	Name        string
	Description string
}

// settingDefs is the single source of truth for known settings. The settings
// tool description is generated from it, so adding a setting here automatically
// exposes it to the agent.
var settingDefs = []SettingDef{
	{
		Name:        "shellPath",
		Description: "Shell binary used by the bash tool (e.g. pwsh, nu, cmd.exe). Supports ~ expansion and bare command names resolved on PATH.",
	},
	{
		Name:        "shellCommandPrefix",
		Description: "Command prepended to every bash invocation.",
	},
	{
		Name:        "shellEncoding",
		Description: "Encoding used to decode bash output. Set this to gbk (or another non-UTF-8 codepage) when using PowerShell/cmd on a CJK system to avoid garbled Chinese. Supported: utf-8, gbk, gb18030, big5, shift-jis, euc-jp, euc-kr, latin1, windows-1252.",
	},
}

// SettingsToolDescription builds the settings tool description by expanding all
// known settings (with their current values, if store is provided).
func SettingsToolDescription(store *SettingsStore) string {
	var b strings.Builder
	b.WriteString("Read and update agent settings. Configure the agent itself, for example which shell the bash tool uses or how its output is decoded. Supported settings:\n")
	for _, def := range settingDefs {
		b.WriteString("- " + def.Name + ": " + def.Description)
		if store != nil {
			if v, ok := store.Get(def.Name); ok {
				b.WriteString(fmt.Sprintf(" (current: %v)", v))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Settings are persisted in the user config file (~/.tau-tool/settings.json) and apply to all future tool calls. Use set to change a setting, get to read the current value, unset to remove it.")
	return b.String()
}

// SettingsToolPromptSnippet and guidelines mirror pi's system prompt style.
const (
	SettingsToolPromptSnippet = "Read and update agent settings"
	SettingsToolGuidelines    = "Configure the shell or other agent settings with settings instead of asking the user"
)

// SettingsToolGuidelineList exposes the settings guidelines as a list.
func SettingsToolGuidelineList() []string {
	return strings.Split(SettingsToolGuidelines, "\n")
}

// Settings is a generic JSON settings object.
type Settings map[string]any

// deepMerge returns base with overrides applied recursively (overrides win).
func deepMerge(base, overrides Settings) Settings {
	result := Settings{}
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overrides {
		if baseVal, ok := base[k]; ok {
			if baseMap, ok1 := baseVal.(map[string]any); ok1 {
				if overrideMap, ok2 := v.(map[string]any); ok2 {
					result[k] = deepMerge(baseMap, overrideMap)
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}

// SettingsStore loads and persists a single user-level settings file.
type SettingsStore struct {
	mu       sync.Mutex
	path     string
	settings Settings
}

// configDirName is the user config directory name.
const configDirName = ".tau-tool"

// EnvSettings is the environment variable that overrides the settings file
// path. When unset, settings live in ~/.tau-tool/settings.json.
const EnvSettings = "TAU_TOOL_SETTINGS"

// NewSettingsStore loads settings from TAU_TOOL_SETTINGS if set, otherwise
// ~/.tau-tool/settings.json.
func NewSettingsStore() *SettingsStore {
	if p := os.Getenv(EnvSettings); p != "" {
		return NewSettingsStoreWithPath(p)
	}
	home, _ := os.UserHomeDir()
	return NewSettingsStoreWithPath(filepath.Join(home, configDirName, "settings.json"))
}

// NewSettingsStoreWithPath is a test helper for a custom settings file path.
func NewSettingsStoreWithPath(path string) *SettingsStore {
	return &SettingsStore{path: path, settings: loadSettingsFile(path)}
}

func loadSettingsFile(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}
	}
	return s
}

// All returns the current settings.
func (s *SettingsStore) All() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return deepMerge(Settings{}, s.settings)
}

// Get returns the value for a key.
func (s *SettingsStore) Get(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.settings[key]
	return v, ok
}

// GetString returns the string value for a key, or "" if absent.
func (s *SettingsStore) GetString(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.settings[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

// Set writes a key to the settings file.
func (s *SettingsStore) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return saveSettingsFile(s.path, s.settings)
}

// Unset removes a key from the settings file.
func (s *SettingsStore) Unset(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.settings, key)
	return saveSettingsFile(s.path, s.settings)
}

// saveSettingsFile writes settings atomically (temp file + rename), creating
// parent directories. Missing files are created; empty settings write an empty
// object so the file remains valid JSON.
func saveSettingsFile(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ResolveShellPath applies ~ expansion to a configured shellPath.
func ResolveShellPath(shellPath string) string {
	return resolvePath(shellPath, "")
}

// EffectiveShell returns the settings-driven shell override, command prefix,
// and output encoding.
func (s *SettingsStore) EffectiveShell() (shellPath, commandPrefix, outputEncoding string) {
	shellPath = s.GetString("shellPath")
	if shellPath != "" {
		shellPath = ResolveShellPath(shellPath)
	}
	commandPrefix = s.GetString("shellCommandPrefix")
	outputEncoding = s.GetString("shellEncoding")
	return
}

type settingsDeps struct {
	store *SettingsStore
}

// CreateSettingsTool returns the settings tool handler bound to a store.
func CreateSettingsTool(store *SettingsStore) mcp.ToolHandlerFor[SettingsInput, any] {
	if store == nil {
		store = NewSettingsStore()
	}
	deps := settingsDeps{store: store}
	return deps.handle
}

func (d settingsDeps) handle(_ context.Context, _ *mcp.CallToolRequest, in SettingsInput) (*mcp.CallToolResult, any, error) {
	op := in.Operation
	if op == "" {
		return toolError("settings: operation is required (get, set, unset)"), nil, nil
	}

	switch op {
	case "get":
		if in.Key == "" {
			return toolError("settings: key is required for get"), nil, nil
		}
		value, ok := d.store.Get(in.Key)
		if !ok {
			return toolError(fmt.Sprintf("Setting %q not found.", in.Key)), nil, nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return toolError(err.Error()), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil

	case "set":
		if in.Key == "" {
			return toolError("settings: key is required for set"), nil, nil
		}
		if in.Value == nil {
			return toolError(fmt.Sprintf("settings: value is required for set (%s)", in.Key)), nil, nil
		}
		if err := d.store.Set(in.Key, in.Value); err != nil {
			return toolError(err.Error()), nil, nil
		}
		data, _ := json.Marshal(in.Value)
		text := fmt.Sprintf("Set %s = %s. Saved to %s.", in.Key, data, d.store.path)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil

	case "unset":
		if in.Key == "" {
			return toolError("settings: key is required for unset"), nil, nil
		}
		if err := d.store.Unset(in.Key); err != nil {
			return toolError(err.Error()), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unset %s.", in.Key)}}}, nil, nil

	default:
		return toolError(fmt.Sprintf("Invalid operation: %s. Must be get, set, or unset.", op)), nil, nil
	}
}
