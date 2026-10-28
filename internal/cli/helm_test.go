package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
)

func TestHelmDemoGroupingAndColumns(t *testing.T) {
	out, err := run(t, "top", "-g", "helm", "-A", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Rows []*model.Row }
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	names := map[string]*model.Row{}
	for _, row := range doc.Rows {
		names[row.Name] = row
	}
	for _, name := range []string{"shop · prod", "database · prod", "Not managed by Helm", "Unknown Helm release · monitoring"} {
		if names[name] == nil {
			t.Fatalf("missing demo group %s: %s", name, out)
		}
	}
	if names["shop · prod"].Helm == nil || names["shop · prod"].Helm.Release != "shop" {
		t.Fatal("JSON export omitted Helm metadata")
	}
	if len(names["shop · prod"].Children) != 2 {
		t.Fatal("same release's workloads were not grouped")
	}
	out, err = run(t, "top", "--show-helm", "--sort-by", "helm-version")
	if err != nil || !strings.Contains(out, "HELM CHART") || !strings.Contains(out, "postgresql") {
		t.Fatalf("Helm columns: %v %s", err, out)
	}
	for _, key := range []string{"helm-chart", "helm-namespace", "helm-version"} {
		if _, err := run(t, "top", "--sort-by", key); err == nil || !strings.Contains(err.Error(), "--show-helm") {
			t.Fatalf("hidden explicit sort %s not rejected: %v", key, err)
		}
		if _, err := run(t, "storage", "top", "--show-helm", "--sort-by", key); err != nil {
			t.Fatalf("storage sort %s: %v", key, err)
		}
	}
}

func TestHelmConfigDefaultsAndSortOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	config := `show_helm = true
sort_order = ["helm-chart", "helm-namespace", "helm-version", "name"]
`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := executeSettings(t, "--config", path, "top", "--demo", "--no-color")
	if err != nil || !strings.Contains(out, "HELM CHART") {
		t.Fatalf("Helm settings: %v %s", err, out)
	}
	out, err = executeSettings(t, "--config", path, "top", "--demo", "--no-color", "--show-helm=false")
	if err != nil || strings.Contains(out, "HELM CHART") {
		t.Fatalf("Helm override / hidden cycle: %v %s", err, out)
	}
}
