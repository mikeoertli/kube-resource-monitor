package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestRefreshBannerRequiresRepeatedSlowCollectionsAndRecovery(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	deliver(m, m.collect())
	baseline := m.visibleRows()
	sample := func(d time.Duration, err error) {
		m.Update(snapshotMsg{snap: testSnapshot(), duration: d, err: err, generation: m.generation})
	}
	for i := 0; i < 2; i++ {
		sample(7*time.Second, nil)
	}
	if strings.Contains(m.View(), "Refresh interval too fast") {
		t.Fatal("warned before three slow samples")
	}
	// A fast sample interrupts the slow streak.
	sample(time.Second, nil)
	for i := 0; i < 2; i++ {
		sample(7*time.Second, nil)
	}
	if m.refreshWarning {
		t.Fatal("slow streak did not reset")
	}
	sample(7*time.Second, nil)
	out := m.View()
	for _, text := range []string{"Refresh interval too fast", "7s", "interval 5s", "Press -", "--interval 9s", "web"} {
		if !strings.Contains(out, text) {
			t.Fatalf("missing %q in warning view: %s", text, out)
		}
	}
	if m.visibleRows() >= baseline {
		t.Fatal("banner did not reserve table space")
	}
	sample(time.Second, nil)
	sample(time.Second, nil)
	if !m.refreshWarning {
		t.Fatal("cleared before sustained recovery")
	}
	sample(time.Second, context.DeadlineExceeded)
	sample(time.Second, nil)
	if !m.refreshWarning {
		t.Fatal("fast failure counted as recovery")
	}
	sample(time.Second, nil)
	sample(time.Second, nil)
	if m.refreshWarning || m.visibleRows() != baseline {
		t.Fatal("warning did not clear after recovery")
	}
}

func TestRefreshWarningIgnoresStaleCollectionsAndCountsTimeouts(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	for i := 0; i < 3; i++ {
		m.Update(snapshotMsg{duration: 20 * time.Second, generation: m.generation - 1})
	}
	if m.refreshWarning {
		t.Fatal("stale samples triggered warning")
	}
	for i := 0; i < 3; i++ {
		m.Update(snapshotMsg{duration: 20 * time.Second, err: context.DeadlineExceeded, generation: m.generation})
	}
	if !m.refreshWarning || !strings.Contains(m.View(), "error:") {
		t.Fatal("timeouts must show error and slow refresh warning")
	}
}

func TestRefreshBannerWrapsAtNarrowWidths(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	m.width = 48
	for i := 0; i < 3; i++ {
		m.observeRefresh(7*time.Second, nil)
	}
	banner := m.refreshBanner()
	for _, line := range strings.Split(banner, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("banner overflow: %s", line)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(banner), " "), "--interval 9s") {
		t.Fatalf("wrapped banner lost recommendation: %q", banner)
	}
}

func TestChangingRefreshIntervalRestartsDetection(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	for i := 0; i < 3; i++ {
		m.observeRefresh(7*time.Second, nil)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if m.cfg.Interval <= 5*time.Second || m.refreshWarning || m.slowRefreshes != 0 {
		t.Fatal("changing cadence should restart warning detection")
	}
}

func TestCollectionMeasuresElapsedTimeAndMarksInitialLoad(t *testing.T) {
	m := newTestModel(t, &stubCollector{snap: testSnapshot()})
	m.Init()
	if !m.loading {
		t.Fatal("initial collection must block overlapping timer refreshes")
	}
	msg := m.collect()().(snapshotMsg)
	if msg.duration <= 0 {
		t.Fatal("collection did not record elapsed time")
	}
	m.Update(msg)
	if m.loading {
		t.Fatal("completed collection still marked loading")
	}
}
