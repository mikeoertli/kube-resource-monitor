package cli

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeSettings(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestConfigDefaultsAndExplicitCLIOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "requests: false\nlimits: false\nshow_age: false\nshow_restarts: false\nsort_order: [memory, cpu]\ninterval: 9s\nstorage_interval: 3m\nstorage_sort_order: [request, use%, kind, used]\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeSettings(t, "--config", path, "top", "--demo", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"CPU REQ", "CPU LIM", "AGE", "RESTARTS"} {
		if strings.Contains(out, header) {
			t.Fatalf("config false ignored: %s", out)
		}
	}
	out, err = executeSettings(t, "--config", path, "top", "--demo", "--no-color", "--requests", "--limits", "--show-age", "--show-restarts")
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"CPU REQ", "CPU LIM", "AGE", "RESTARTS"} {
		if !strings.Contains(out, header) {
			t.Fatalf("CLI override lost: %s", out)
		}
	}
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--config", path, "storage", "top", "--demo", "--no-color"})
	var sink bytes.Buffer
	cmd.SetOut(&sink)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, _ := cmd.Flags().GetString("sort-by"); got != "request" {
		t.Fatalf("storage default sort %s", got)
	}
	if got, _ := cmd.Flags().GetDuration("interval"); got.String() != "3m0s" {
		t.Fatalf("storage interval %s", got)
	}
}

func TestPrintConfigNeverReadsUserSettingsOrCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.yaml")
	_ = os.WriteFile(path, []byte("token: SUPERSECRET\ninvalid: ["), 0600)
	out, err := executeSettings(t, "--config", path, "--print-config")
	if err != nil || strings.Contains(out, "SUPERSECRET") {
		t.Fatalf("print config read secrets: %v", err)
	}
	var prefs map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &prefs); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"requests", "limits", "show_age", "show_restarts"} {
		if prefs[key] != true {
			t.Fatalf("default %s is not true", key)
		}
	}
	for _, key := range []string{"sort_order", "storage_sort_order", "interval", "storage_interval", "kubeconfig", "context", "threshold", "bars", "include_missing", "reverse", "csv"} {
		if _, ok := prefs[key]; !ok {
			t.Fatalf("missing setting %s", key)
		}
	}
	if _, ok := prefs["init_config"]; ok {
		t.Fatal("action saved as a preference")
	}
}

func TestInitExistingConfigOpensDiffWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.yaml")
	original := "requests: false\n"
	_ = os.WriteFile(path, []byte(original), 0600)
	editor := filepath.Join(dir, "fake editor")
	capture := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$KRM_TEST_ARGS\"\n"
	if err := os.WriteFile(editor, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KRM_DIFF_EDITOR", fmt.Sprintf("%q --diff", editor))
	t.Setenv("KRM_TEST_ARGS", capture)
	out, err := executeSettings(t, "--config", path, "--init-config")
	if err != nil {
		t.Fatalf("init diff: %v %s", err, out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("existing config overwritten")
	}
	captured, _ := os.ReadFile(capture)
	args := strings.Split(strings.TrimSpace(string(captured)), "\n")
	if len(args) != 3 || args[0] != "--diff" || args[2] != path || args[1] == path {
		t.Fatalf("config must be the right/destination file: %v", args)
	}
}

func TestInitConfigAndEditRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "config.yaml")
	if _, err := executeSettings(t, "--config", path, "--init-config"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("bad config permissions: %v", err)
	}
	// A malformed file must still open in the editor and be validated afterward.
	_ = os.WriteFile(path, []byte("invalid: ["), 0600)
	editor := filepath.Join(dir, "repair")
	script := "#!/bin/sh\nfor destination do :; done\nprintf 'requests: false\\n' > \"$destination\"\n"
	_ = os.WriteFile(editor, []byte(script), 0700)
	t.Setenv("EDITOR", editor)
	if _, err := executeSettings(t, "--config", path, "--edit-config"); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSettingsFailBeforeClusterAccess(t *testing.T) {
	for _, data := range []string{"requests: maybe", "sort_order: []", "sort_order: [cpu%, cpu%]", "storage_sort_order: [cpu]", "typo: true", "interval: 100ms"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		_ = os.WriteFile(path, []byte(data), 0600)
		if _, err := executeSettings(t, "--config", path, "top", "--kubeconfig", "/missing/kubeconfig"); err == nil || strings.Contains(err.Error(), "/missing/kubeconfig") {
			t.Fatalf("did not validate %q first: %v", data, err)
		}
	}
}

func TestVersionFlagsMatchVersionCommandWithoutConfig(t *testing.T) {
	want, err := executeSettings(t, "version", "--config", "/missing/config")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"-v", "--version"} {
		out, err := executeSettings(t, flag, "--config", "/missing/config")
		if err != nil || out != want {
			t.Fatalf("version alias %s: %v %s", flag, err, out)
		}
	}
}

func TestNotificationConfigDefaultsAndCLIOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("notify:\n  on: [\"cpu>10000m\"]\n  stdout: true\n  hysteresis: 0\n"), 0600)
	out, err := executeSettings(t, "--config", path, "notify", "--demo", "--once")
	if err != nil || !strings.Contains(out, "cpu > 10.0") || !strings.Contains(out, "stdout") {
		t.Fatalf("notify defaults: %v %s", err, out)
	}
	out, err = executeSettings(t, "--config", path, "notify", "--demo", "--once", "--on", "cpu>20000m")
	if err != nil || !strings.Contains(out, "cpu > 20.0") || strings.Contains(out, "cpu > 10.0") {
		t.Fatalf("notify override: %v %s", err, out)
	}
}

func TestGeneratedTemplateLoadsEveryDefault(t *testing.T) {
	data, err := configTemplate()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, data, 0600)
	if _, err := executeSettings(t, "--config", path, "top", "--demo", "--no-color"); err != nil {
		t.Fatal(err)
	}
}
