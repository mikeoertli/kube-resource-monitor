package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/kube-resource-monitor/internal/inventory"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
)

func TestHelmColumnsToggleSortAvailabilityAndRefetch(t *testing.T) {
	stub := &stubCollector{snap: testSnapshot()}
	m := newTestModel(t, stub)
	deliver(m, m.collect())
	if strings.Contains(m.View(), "HELM CHART") {
		t.Fatal("Helm columns must default off")
	}
	for _, key := range m.sortChoices() {
		if model.IsHelmSortKey(key) {
			t.Fatal("hidden Helm column in sort cycle")
		}
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	deliver(m, cmd)
	if !stub.lastCall().IncludeHelm || !m.cfg.Render.ShowHelm || !strings.Contains(m.View(), "HELM CHART") {
		t.Fatal("Helm toggle did not show/refetch columns")
	}
	m.cfg.Sort = model.SortName
	m.cycleSort()
	if m.cfg.Sort != model.SortHelmChart {
		t.Fatal("visible Helm sorts missing from default cycle")
	}
	m.cfg.SortOrder = []model.SortKey{model.SortHelmVersion, model.SortName}
	m.cfg.Sort = model.SortHelmVersion
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	deliver(m, cmd)
	if m.cfg.Render.ShowHelm || stub.lastCall().IncludeHelm || m.cfg.Sort != model.SortName {
		t.Fatal("turning off columns kept a hidden Helm sort")
	}
}

func TestHelmGroupingInCycleAndColumnsStayOptional(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	m.cfg.Options.GroupBy = inventory.GroupWorkload
	m.cycleGroup()
	if m.cfg.Options.GroupBy != inventory.GroupHelm {
		t.Fatal("Helm grouping missing from group cycle")
	}
	if m.cfg.Render.ShowHelm {
		t.Fatal("Helm grouping forced optional columns on")
	}
}

func TestHelmMetadataWarningsReserveTableSpace(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	deliver(m, m.collect())
	baseline := m.visibleRows()
	m.snapshot.Warnings = []string{"Helm metadata incomplete: cannot list Deployment", "Helm metadata incomplete: cannot list CronJob"}
	if m.visibleRows() != baseline-2 {
		t.Fatal("metadata permission warnings push footer off screen")
	}
	if !strings.Contains(m.View(), "Helm metadata incomplete") {
		t.Fatal("metadata warnings not shown")
	}
}
