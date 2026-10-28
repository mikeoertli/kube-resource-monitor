package render

import (
	"strings"
	"testing"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
)

func TestHelmColumnsAreOptionalAndTrailing(t *testing.T) {
	flat := []model.FlatRow{{Row: &model.Row{Name: "example", Helm: &model.HelmMetadata{Managed: true, Chart: "my-chart", Namespace: "release-ns", ChartVersion: "1.2.3"}}}, {Row: &model.Row{Name: "unmanaged"}}}
	opts := DefaultOptions()
	opts.ShowAge = true
	opts.ShowLabels = true
	table := NewTable(NewPalette(false), opts)
	header, _ := table.Lines(flat)
	if strings.Contains(header, "HELM") {
		t.Fatal("Helm columns default on")
	}
	opts.ShowHelm = true
	table = NewTable(NewPalette(false), opts)
	header, lines := table.Lines(flat)
	for _, want := range []string{"HELM CHART", "HELM NS", "CHART VERSION"} {
		if !strings.Contains(header, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if !strings.HasSuffix(header, "CHART VERSION") || strings.Index(header, "HELM CHART") < strings.Index(header, "LABELS") {
		t.Fatal("Helm columns must appear at the end")
	}
	for _, want := range []string{"my-chart", "release-ns", "1.2.3"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("missing value %s", want)
		}
	}
	if strings.Contains(lines[1], "my-chart") {
		t.Fatal("unmanaged row acquired unrelated metadata")
	}
}
