package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func settingsToolResult(t *testing.T, store *SettingsStore, in SettingsInput) string {
	t.Helper()
	handler := CreateSettingsTool(store)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, in)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return textOf(result)
}

func TestSettingsSetGetUnset(t *testing.T) {
	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))

	got := settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath", Value: "pwsh"})
	if !strings.Contains(got, "shellPath = \"pwsh\"") {
		t.Errorf("set result = %q", got)
	}

	got = settingsToolResult(t, store, SettingsInput{Operation: "get", Key: "shellPath"})
	if got != `"pwsh"` {
		t.Errorf("get result = %q", got)
	}

	got = settingsToolResult(t, store, SettingsInput{Operation: "unset", Key: "shellPath"})
	if !strings.Contains(got, "Unset shellPath") {
		t.Errorf("unset result = %q", got)
	}

	got = settingsToolResult(t, store, SettingsInput{Operation: "get", Key: "shellPath"})
	if !strings.Contains(got, "not found") {
		t.Errorf("expected not found, got %q", got)
	}
}

func TestSettingsDescriptionExpandsAllSettings(t *testing.T) {
	desc := SettingsToolDescription(nil)
	for _, def := range settingDefs {
		if !strings.Contains(desc, "- "+def.Name+":") {
			t.Errorf("description missing setting %s: %q", def.Name, desc)
		}
	}
	if strings.Contains(desc, "list") {
		t.Errorf("description should not mention the removed list operation: %q", desc)
	}
}

func TestSettingsDescriptionShowsCurrentValues(t *testing.T) {
	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Set("shellPath", "pwsh"); err != nil {
		t.Fatalf("set: %v", err)
	}
	desc := SettingsToolDescription(store)
	if !strings.Contains(desc, "shellPath:") || !strings.Contains(desc, "pwsh") {
		t.Errorf("description should show current value: %q", desc)
	}
}

func TestSettingsPersistsToFile(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), ".tau-tool", "settings.json")
	store := NewSettingsStoreWithPath(settingsPath)

	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellCommandPrefix", Value: "set -e"})

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("settings file invalid JSON: %v", err)
	}
	if raw["shellCommandPrefix"] != "set -e" {
		t.Errorf("file contents = %v", raw)
	}
}

func TestSettingsReloadsFromFile(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	store := NewSettingsStoreWithPath(settingsPath)
	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath", Value: "bash"})

	// A fresh store must see the persisted value.
	fresh := NewSettingsStoreWithPath(settingsPath)
	value, ok := fresh.Get("shellPath")
	if !ok || value != "bash" {
		t.Errorf("reloaded shellPath = %v (ok=%v), want bash", value, ok)
	}
}

func TestSettingsInvalidOperation(t *testing.T) {
	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))

	got := settingsToolResult(t, store, SettingsInput{Operation: "bogus"})
	if !strings.Contains(got, "Invalid operation") {
		t.Errorf("result = %q", got)
	}

	got = settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath"})
	if !strings.Contains(got, "value is required") {
		t.Errorf("result = %q", got)
	}
}

func TestBashUsesSettingsShell(t *testing.T) {
	shell := shellForTest(t)
	if shell == "" {
		return
	}

	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))
	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath", Value: shell})

	deps := bashDeps{settings: store}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo via-settings"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash failed: %s", textOf(result))
	}
	if !strings.Contains(textOf(result), "via-settings") {
		t.Errorf("output = %q", textOf(result))
	}
}

func TestBashUsesSettingsCommandPrefix(t *testing.T) {
	shell := shellForTest(t)
	if shell == "" {
		return
	}

	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))
	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath", Value: shell})
	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellCommandPrefix", Value: "export GREETING=hi"})

	deps := bashDeps{settings: store}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo $GREETING"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash failed: %s", textOf(result))
	}
	if !strings.Contains(textOf(result), "hi") {
		t.Errorf("output = %q (prefix not applied)", textOf(result))
	}
}

func TestSettingsExplicitOptionBeatsSettings(t *testing.T) {
	shell := shellForTest(t)
	if shell == "" {
		return
	}

	store := NewSettingsStoreWithPath(filepath.Join(t.TempDir(), "settings.json"))
	settingsToolResult(t, store, SettingsInput{Operation: "set", Key: "shellPath", Value: "definitely-not-a-shell"})

	// An explicit shellPath option must win over the bad setting.
	deps := bashDeps{shellPath: shell, settings: store}
	result, _, err := deps.handle(context.Background(), &mcp.CallToolRequest{}, BashInput{Command: "echo ok"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash failed: %s", textOf(result))
	}
}
