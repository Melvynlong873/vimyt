package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Sadoaz/vimyt/internal/model"
)

type historyModel struct {
	playHistory *model.PlayHistory // shared pointer, set by App
	cursor      int
	scroll      int // viewport scroll offset
	width       int
	height      int

	// Visual select
	visual bool
	anchor int

	// Filter
	filterInput textinput.Model
	filterQuery string
	filteredIdx []int // indices into tracks() result that match the filter

	// Undo/redo stacks for play history
	undoStack [][]model.PlayHistoryEntry
	redoStack [][]model.PlayHistoryEntry

	// Favorites set (track IDs) — set by App before rendering
	favSet map[string]bool
	// Tick counter for marquee animation — set by App before rendering
	tick int
	// Whether this panel is currently focused — set by App before rendering
	focused bool
	// Whether to show relative line numbers — set by App before rendering
	relNumbers bool
}

func newHistoryModel(ph *model.PlayHistory) historyModel {
	ti := textinput.New()
	ti.CharLimit = 80
	return historyModel{
		playHistory: ph,
		filterInput: ti,
	}
}

// --- Navigation ---

func (m *historyModel) tracks() []model.Track {
	if m.playHistory == nil {
		return nil
	}
	return m.playHistory.Tracks()
}

func (m *historyModel) ensureVisible() {
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

func (m *historyModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.ensureVisible()
	}
}

func (m *historyModel) moveDown() {
	tracks := m.visibleTracks()
	if m.cursor < len(tracks)-1 {
		m.cursor++
		m.ensureVisible()
	}
}

func (m *historyModel) goTop() {
	m.cursor = 0
	m.scroll = 0
}

func (m *historyModel) goBottom() {
	tracks := m.visibleTracks()
	if len(tracks) > 0 {
		m.cursor = len(tracks) - 1
		m.ensureVisible()
	}
}

func (m *historyModel) halfPageDown(visibleHeight int) {
	half := max(visibleHeight/2, 1)
	m.cursor += half
	tracks := m.visibleTracks()
	m.cursor = min(m.cursor, len(tracks)-1)
	m.cursor = max(m.cursor, 0)
	m.ensureVisible()
}

func (m *historyModel) halfPageUp(visibleHeight int) {
	half := max(visibleHeight/2, 1)
	m.cursor -= half
	m.cursor = max(m.cursor, 0)
	m.ensureVisible()
}

// --- Visual select ---

func (m *historyModel) toggleVisual() {
	m.visual = !m.visual
	if m.visual {
		m.anchor = m.cursor
	}
}

func (m *historyModel) swapVisualEnd() {
	if m.visual {
		m.anchor, m.cursor = m.cursor, m.anchor
	}
}

func (m *historyModel) isSelected(i int) bool {
	if !m.visual {
		return false
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

func (m *historyModel) yankSelected() []model.Track {
	tracks := m.visibleTracks()
	if len(tracks) == 0 {
		return nil
	}
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

func (m *historyModel) saveUndo() {
	snap := make([]model.PlayHistoryEntry, len(m.playHistory.Entries))
	copy(snap, m.playHistory.Entries)
	m.undoStack = append(m.undoStack, snap)
	m.redoStack = nil
}

func (m *historyModel) performUndo() bool {
	if len(m.undoStack) == 0 {
		return false
	}
	// Save current to redo
	redoSnap := make([]model.PlayHistoryEntry, len(m.playHistory.Entries))
	copy(redoSnap, m.playHistory.Entries)
	m.redoStack = append(m.redoStack, redoSnap)
	// Restore from undo
	snap := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]
	m.playHistory.Entries = snap
	m.playHistory.Save()
	m.visual = false
	if m.isFiltered() {
		m.liveFilter()
	}
	total := len(m.visibleTracks())
	if m.cursor >= total && m.cursor > 0 {
		m.cursor = total - 1
	}
	return true
}

func (m *historyModel) performRedo() bool {
	if len(m.redoStack) == 0 {
		return false
	}
	undoSnap := make([]model.PlayHistoryEntry, len(m.playHistory.Entries))
	copy(undoSnap, m.playHistory.Entries)
	m.undoStack = append(m.undoStack, undoSnap)
	snap := m.redoStack[len(m.redoStack)-1]
	m.redoStack = m.redoStack[:len(m.redoStack)-1]
	m.playHistory.Entries = snap
	m.playHistory.Save()
	m.visual = false
	if m.isFiltered() {
		m.liveFilter()
	}
	total := len(m.visibleTracks())
	if m.cursor >= total && m.cursor > 0 {
		m.cursor = total - 1
	}
	return true
}

// deleteAtCursor removes the track at the cursor position from play history.
func (m *historyModel) deleteAtCursor() {
	visible := m.visibleTracks()
	if len(visible) == 0 || m.cursor < 0 || m.cursor >= len(visible) {
		return
	}
	// Map visible index to the real display index, then to internal index
	displayIdx := m.realIndex(m.cursor)
	// Display is reverse chronological: display index i = internal index (len-1-i)
	internalIdx := m.playHistory.Len() - 1 - displayIdx
	m.playHistory.Remove(internalIdx)
	// Refresh filter if active
	if m.isFiltered() {
		m.liveFilter()
	}
	// Clamp cursor
	newTotal := len(m.visibleTracks())
	if m.cursor >= newTotal && m.cursor > 0 {
		m.cursor = newTotal - 1
	}
	m.ensureVisible()
}

// deleteVisual removes all tracks in the visual selection from play history.
func (m *historyModel) deleteVisual() {
	visible := m.visibleTracks()
	if len(visible) == 0 || !m.visual {
		m.visual = false
		return
	}
	lo, hi := m.anchor, m.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	hi = min(hi, len(visible)-1)

	if m.isFiltered() {
		// Map visible indices to real display indices, then to internal indices.
		// Remove from high to low to avoid index shifting.
		n := m.playHistory.Len()
		for i := hi; i >= lo; i-- {
			displayIdx := m.realIndex(i)
			internalIdx := n - 1 - displayIdx
			m.playHistory.Remove(internalIdx)
			n = m.playHistory.Len()
		}
		m.liveFilter()
	} else {
		// Convert display range [lo, hi] to internal range.
		// Display is reversed, so display lo..hi maps to internal (len-1-hi)..(len-1-lo).
		n := m.playHistory.Len()
		internalLo := n - 1 - hi
		internalHi := n - 1 - lo
		m.playHistory.RemoveRange(internalLo, internalHi)
	}

	m.visual = false
	m.cursor = lo
	newTotal := len(m.visibleTracks())
	if m.cursor >= newTotal && m.cursor > 0 {
		m.cursor = newTotal - 1
	}
	m.ensureVisible()
}

// pasteAfterCursor inserts tracks after the cursor position in play history.
func (m *historyModel) pasteAfterCursor(tracks []model.Track) {
	if len(tracks) == 0 || m.playHistory == nil {
		return
	}
	m.saveUndo()

	// Map visible cursor to display index, then to internal index.
	// Display is reverse chronological: display index i = internal index (len-1-i).
	// "After cursor" in display (newest-first) = before in internal (oldest-first).
	displayIdx := m.realIndex(m.cursor)
	internalIdx := m.playHistory.Len() - 1 - displayIdx

	// Convert tracks to PlayHistoryEntry (inserted in display order = reverse internal)
	entries := make([]model.PlayHistoryEntry, len(tracks))
	for i, t := range tracks {
		entries[i] = model.PlayHistoryEntry{
			TrackID:  t.ID,
			Title:    t.Title,
			Artist:   t.Artist,
			Duration: t.Duration,
			PlayedAt: time.Now(),
			Source:   "paste",
		}
	}
	// Reverse entries so display order matches the clipboard order
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	newEntries := make([]model.PlayHistoryEntry, 0, m.playHistory.Len()+len(entries))
	newEntries = append(newEntries, m.playHistory.Entries[:internalIdx]...)
	newEntries = append(newEntries, entries...)
	newEntries = append(newEntries, m.playHistory.Entries[internalIdx:]...)
	m.playHistory.Entries = newEntries
	m.playHistory.Save()

	if m.isFiltered() {
		m.liveFilter()
	}
	m.cursor++
	m.ensureVisible()
}

// --- Filter ---

func (m *historyModel) startFilter() tea.Cmd {
	m.filterInput.SetValue("")
	m.filterInput.Placeholder = "Filter history..."
	return m.filterInput.Focus()
}

func (m *historyModel) liveFilter() {
	query := strings.TrimSpace(m.filterInput.Value())
	m.filterQuery = query

	if query == "" {
		m.filteredIdx = nil
		m.filterQuery = ""
		m.cursor = 0
		return
	}

	allTracks := m.playHistory.Tracks()
	m.filteredIdx = nil
	for i, t := range allTracks {
		if fuzzyMatch(t.Title, t.Artist, query) {
			m.filteredIdx = append(m.filteredIdx, i)
		}
	}
	m.cursor = 0
	m.visual = false
}

func (m *historyModel) confirmFilter() {
	m.liveFilter()
	m.filterInput.Blur()
}

func (m *historyModel) clearFilter() {
	m.filterQuery = ""
	m.filteredIdx = nil
}

func (m *historyModel) isFiltered() bool {
	return m.filterQuery != ""
}

func (m *historyModel) isFilterActive() bool {
	return m.filterInput.Focused()
}

// visibleTracks returns the filtered or full track list.
func (m *historyModel) visibleTracks() []model.Track {
	allTracks := m.tracks()
	if m.isFiltered() {
		var filtered []model.Track
		for _, idx := range m.filteredIdx {
			if idx < len(allTracks) {
				filtered = append(filtered, allTracks[idx])
			}
		}
		return filtered
	}
	return allTracks
}

// realIndex maps a visible-list index to the actual history index.
func (m *historyModel) realIndex(visibleIdx int) int {
	if m.isFiltered() && visibleIdx < len(m.filteredIdx) {
		return m.filteredIdx[visibleIdx]
	}
	return visibleIdx
}

func (m *historyModel) currentTrack() *model.Track {
	tracks := m.visibleTracks()
	if m.cursor < 0 || m.cursor >= len(tracks) {
		return nil
	}
	t := tracks[m.cursor]
	return &t
}

// ViewCompact returns a single-line summary for when the panel is not focused.
func (m historyModel) ViewCompact(width int) string {
	count := 0
	if m.playHistory != nil {
		count = len(m.playHistory.Tracks())
	}
	line := fmt.Sprintf("  %d tracks", count)
	if width > 0 {
		line = ansi.Truncate(line, width, "")
	}
	return line + "\n"
}

// --- Rendering ---

var (
	histNormalStyle = lipgloss.NewStyle().Padding(0, 2)
	histCursorStyle = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	histSelStyle    = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	histBothStyle   = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238")).Bold(true)
)

func (m historyModel) ViewConstrained(width, height int) string {
	var b strings.Builder

	tracks := m.visibleTracks()

	if len(tracks) == 0 {
		if m.isFiltered() {
			b.WriteString("  No matching tracks.")
		} else {
			b.WriteString("  No play history yet.")
		}
		return b.String()
	}

	if m.isFiltered() {
		filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
		b.WriteString(filterStyle.Render(fmt.Sprintf("  (filter: %s)", m.filterQuery)))
		b.WriteString("\n\n")
	}

	maxVisible := height
	if m.isFiltered() {
		maxVisible -= 2 // filter label + blank line
	}
	if maxVisible < 1 {
		maxVisible = 10
	}

	// Adjust scroll to keep cursor visible
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+maxVisible {
		m.scroll = m.cursor - maxVisible + 1
	}
	m.scroll = min(m.scroll, len(tracks)-maxVisible)
	m.scroll = max(m.scroll, 0)
	start := m.scroll
	end := min(start+maxVisible, len(tracks))

	for i := start; i < end; i++ {
		t := tracks[i]
		realIdx := m.realIndex(i)
		dur := formatDuration(t.Duration)
		heart := ""
		if m.favSet[t.ID] {
			heart = favStyle.Render(" <3")
		}
		lineNum := realIdx + 1
		if m.relNumbers && i != m.cursor {
			dist := i - m.cursor
			if dist < 0 {
				dist = -dist
			}
			lineNum = dist
		}
		isCursor := i == m.cursor && m.focused
		isSel := m.isSelected(i) && m.focused

		style := histNormalStyle
		prefix := "  "
		switch {
		case isCursor && isSel:
			style = histBothStyle
			prefix = "> "
		case isCursor:
			style = histCursorStyle
			prefix = "> "
		case isSel:
			style = histSelStyle
		}

		var line string
		hasBackground := isCursor || isSel
		if hasBackground {
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
			rendered = marquee(rendered, width, m.tick)
		} else if width > 0 {
			rendered = ansi.Truncate(rendered, width, "")
		}
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	return b.String()
}
