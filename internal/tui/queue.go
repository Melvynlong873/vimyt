package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Sadoaz/vimyt/internal/model"
)

var (
	queueNormalStyle  = lipgloss.NewStyle().Padding(0, 2)
	queueCursorStyle  = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	queuePlayingStyle = lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("48")).Bold(true)
	queueBothStyle    = lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("48")).Bold(true).Background(lipgloss.Color("238"))
	queueSelStyle     = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	queueSelCurStyle  = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238")).Bold(true)
	queueFilterStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
)

type queueModel struct {
	cursor int
	scroll int // viewport scroll offset (first visible line)
	width  int
	height int

	// Visual select
	visual bool
	anchor int

	// Filter
	filterInput textinput.Model
	filterQuery string
	filteredIdx []int // indices into queue.Tracks that match the filter

	// Favorites set (track IDs) — set by App before rendering
	favSet map[string]bool
	// Tick counter for marquee animation — set by App before rendering
	tick int
	// Whether this panel is currently focused — set by App before rendering
	focused bool
	// Whether to show relative line numbers — set by App before rendering
	relNumbers bool
}

func newQueueModel() queueModel {
	ti := textinput.New()
	ti.CharLimit = 80
	return queueModel{
		filterInput: ti,
	}
}

// --- Visual select ---

func (m *queueModel) toggleVisual() {
	m.visual = !m.visual
	if m.visual {
		m.anchor = m.cursor
	}
}

func (m *queueModel) swapVisualEnd() {
	if m.visual {
		m.anchor, m.cursor = m.cursor, m.anchor
	}
}

func (m *queueModel) isSelected(i int) bool {
	if !m.visual {
		return false
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

func (m *queueModel) yankSelected(q *model.Queue) []model.Track {
	if q.Len() == 0 {
		return nil
	}
	tracks := m.visibleTracks(q)
	var result []model.Track
	if m.visual {
		lo, hi := m.anchor, m.cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi && i < len(tracks); i++ {
			result = append(result, tracks[i])
		}
	} else if m.cursor >= 0 && m.cursor < len(tracks) {
		result = append(result, tracks[m.cursor])
	}
	m.visual = false
	return result
}

// deleteVisual removes all tracks in the visual selection from the queue.
func (m *queueModel) deleteVisual(q *model.Queue) {
	if !m.visual {
		m.visual = false
		return
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}

	if m.isFiltered() {
		// Map visible indices to real queue indices, remove from high to low
		for i := hi; i >= lo; i-- {
			if i < len(m.filteredIdx) {
				q.Remove(m.filteredIdx[i])
			}
		}
		m.liveFilter(q)
	} else {
		for i := hi; i >= lo; i-- {
			q.Remove(i)
		}
	}

	m.visual = false
	m.cursor = lo
	m.clampCursor(len(m.visibleTracks(q)))
}

// cutVisual removes visual selection and returns the cut tracks.
func (m *queueModel) cutVisual(q *model.Queue) []model.Track {
	if !m.visual {
		m.visual = false
		return nil
	}
	tracks := m.yankSelected(q) // gets copies, resets visual
	m.visual = true             // re-enable for deleteVisual
	m.deleteVisual(q)
	return tracks
}

// --- Filter ---

func (m *queueModel) startFilter() tea.Cmd {
	m.filterInput.SetValue("")
	m.filterInput.Placeholder = "Filter queue..."
	return m.filterInput.Focus()
}

func (m *queueModel) liveFilter(q *model.Queue) {
	query := strings.TrimSpace(m.filterInput.Value())
	m.filterQuery = query

	if query == "" {
		m.filteredIdx = nil
		m.filterQuery = ""
		m.cursor = 0
		return
	}

	m.filteredIdx = nil
	for i, t := range q.Tracks {
		if fuzzyMatch(t.Title, t.Artist, query) {
			m.filteredIdx = append(m.filteredIdx, i)
		}
	}
	m.cursor = 0
	m.visual = false
}

func (m *queueModel) confirmFilter(q *model.Queue) {
	m.liveFilter(q)
	m.filterInput.Blur()
}

func (m *queueModel) clearFilter() {
	m.filterQuery = ""
	m.filteredIdx = nil
}

func (m *queueModel) isFiltered() bool {
	return m.filterQuery != ""
}

func (m *queueModel) isFilterActive() bool {
	return m.filterInput.Focused()
}

// visibleTracks returns the filtered or full track list.
func (m *queueModel) visibleTracks(q *model.Queue) []model.Track {
	if m.isFiltered() {
		var tracks []model.Track
		for _, idx := range m.filteredIdx {
			if idx < len(q.Tracks) {
				tracks = append(tracks, q.Tracks[idx])
			}
		}
		return tracks
	}
	return q.Tracks
}

// realIndex maps a visible-list index to the actual queue index.
func (m *queueModel) realIndex(visibleIdx int) int {
	if m.isFiltered() && visibleIdx < len(m.filteredIdx) {
		return m.filteredIdx[visibleIdx]
	}
	return visibleIdx
}

// visibleLen returns the number of visible tracks (filtered or full).
func (m *queueModel) visibleLen(q *model.Queue) int {
	if m.isFiltered() {
		return len(m.filteredIdx)
	}
	return q.Len()
}

// --- Navigation ---

// ensureVisible adjusts scroll so the cursor is within the visible viewport.
func (m *queueModel) ensureVisible() {
	h := m.height
	if h < 1 {
		h = 10
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+h {
		m.scroll = m.cursor - h + 1
	}
	m.scroll = max(m.scroll, 0)
}

func (m *queueModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.ensureVisible()
	}
}

func (m *queueModel) moveDown(qLen int) {
	if m.cursor < qLen-1 {
		m.cursor++
		m.ensureVisible()
	}
}

func (m *queueModel) goTop() {
	m.cursor = 0
	m.scroll = 0
}

func (m *queueModel) goBottom(qLen int) {
	if qLen > 0 {
		m.cursor = qLen - 1
		m.ensureVisible()
	}
}

func (m *queueModel) clampCursor(qLen int) {
	m.cursor = min(m.cursor, qLen-1)
	m.cursor = max(m.cursor, 0)
	m.ensureVisible()
}

func (m *queueModel) halfPageDown(qLen, visibleHeight int) {
	half := max(visibleHeight/2, 1)
	m.cursor += half
	m.cursor = min(m.cursor, qLen-1)
	m.cursor = max(m.cursor, 0)
	m.ensureVisible()
}

func (m *queueModel) halfPageUp(visibleHeight int) {
	half := max(visibleHeight/2, 1)
	m.cursor -= half
	m.cursor = max(m.cursor, 0)
	m.ensureVisible()
}

// --- Rendering ---

func renderQueueConstrained(q *model.Queue, qm *queueModel, width, height int) string {
	var b strings.Builder

	tracks := qm.visibleTracks(q)

	if len(tracks) == 0 {
		if qm.isFiltered() {
			b.WriteString("  No matching tracks.")
		} else {
			b.WriteString("  Queue is empty. Search and add tracks with Enter or yy.")
		}
		return b.String()
	}

	if qm.isFiltered() {
		b.WriteString(queueFilterStyle.Render(fmt.Sprintf("  (filter: %s)", qm.filterQuery)))
		b.WriteString("\n\n")
	}

	maxVisible := height

	if qm.isFiltered() {
		maxVisible -= 2 // filter label + blank line
	}
	if maxVisible < 1 {
		maxVisible = 10
	}

	// Adjust scroll to keep cursor visible
	if qm.cursor < qm.scroll {
		qm.scroll = qm.cursor
	}
	if qm.cursor >= qm.scroll+maxVisible {
		qm.scroll = qm.cursor - maxVisible + 1
	}
	qm.scroll = min(qm.scroll, len(tracks)-maxVisible)
	qm.scroll = max(qm.scroll, 0)
	start := qm.scroll
	end := min(start+maxVisible, len(tracks))

	for i := start; i < end; i++ {
		t := tracks[i]
		// Show the real queue number, not the filtered index
		realIdx := qm.realIndex(i)
		dur := formatDuration(t.Duration)
		heart := ""
		if qm.favSet[t.ID] {
			heart = favStyle.Render(" <3")
		}
		lineNum := realIdx + 1
		if qm.relNumbers && i != qm.cursor {
			dist := i - qm.cursor
			if dist < 0 {
				dist = -dist
			}
			lineNum = dist
		}
		isCursor := i == qm.cursor && qm.focused
		isPlaying := realIdx == q.Current
		isSel := qm.isSelected(i) && qm.focused

		style := queueNormalStyle
		prefix := "  "
		switch {
		case isCursor && isSel:
			style = queueSelCurStyle
			prefix = "> "
		case isCursor && isPlaying:
			style = queueBothStyle
			prefix = "> "
		case isCursor:
			style = queueCursorStyle
			prefix = "> "
		case isSel:
			style = queueSelStyle
		case isPlaying:
			style = queuePlayingStyle
		}

		var line string
		hasBackground := isCursor || isSel
		if hasBackground {
			// Plain text for lines with background — uniform color
			line = fmt.Sprintf("%2d  %s  %s  %s",
				lineNum, t.Title, t.Artist, dur)
		} else {
			line = fmt.Sprintf("%2d  %s  %s  %s",
				lineNum,
				t.Title,
				artistStyle.Render(t.Artist),
				durationStyle.Render(dur),
			)
		}
		content := prefix + line + heart
		if isSel && !isCursor && width > 0 {
			content = ansi.Truncate(content, width-4, "")
		}
		rendered := style.Render(content)
		if isCursor && width > 0 {
			rendered = marquee(rendered, width, qm.tick)
		} else if width > 0 {
			rendered = ansi.Truncate(rendered, width, "")
		}
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	return b.String()
}
