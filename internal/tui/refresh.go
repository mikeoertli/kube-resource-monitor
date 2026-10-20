package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const refreshWarningStreak = 3

// Count completed collections, rather than skipped ticks: a single slow request
// should not look like repeated overload. Fast errors do not demonstrate recovery.
func (m *Model) observeRefresh(elapsed time.Duration, err error) {
	if elapsed > m.cfg.Interval {
		m.fastRefreshes = 0
		if m.slowRefreshes < refreshWarningStreak {
			m.slowRefreshes++
		}
		m.slowCollection = elapsed
		if m.slowRefreshes >= refreshWarningStreak {
			m.refreshWarning = true
		}
		return
	}
	m.slowRefreshes = 0
	if err != nil {
		m.fastRefreshes = 0
		return
	}
	if m.fastRefreshes < refreshWarningStreak {
		m.fastRefreshes++
	}
	if m.fastRefreshes >= refreshWarningStreak {
		m.refreshWarning = false
	}
}

func (m *Model) refreshBanner() string {
	if !m.refreshWarning {
		return ""
	}
	// Round up and leave headroom rather than recommending the exact measured time.
	suggested := m.slowCollection + m.slowCollection/4
	if suggested <= m.cfg.Interval {
		suggested = m.cfg.Interval + time.Second
	}
	suggested = ((suggested + time.Second - 1) / time.Second) * time.Second
	message := fmt.Sprintf("! Refresh interval too fast: collection took %s (interval %s). Press - to slow down; try --interval %s.",
		m.slowCollection.Round(100*time.Millisecond), m.cfg.Interval, suggested)
	style := lipgloss.NewStyle()
	if m.width > 0 {
		// Wrap on spaces first so the renderer does not split --interval at
		// its hyphens, which would make the suggested command hard to read.
		var wrapped strings.Builder
		column := 0
		for _, word := range strings.Fields(message) {
			if column > 0 {
				if column+1+len(word) > m.width {
					wrapped.WriteByte('\n')
					column = 0
				} else {
					wrapped.WriteByte(' ')
					column++
				}
			}
			wrapped.WriteString(word)
			column += len(word)
		}
		message = wrapped.String()
		style = style.Width(m.width)
	}
	return m.cfg.Palette.Warning.Render(style.Render(message))
}
