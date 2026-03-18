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

type playlistLevel int

const (
	levelList   playlistLevel = iota // browsing playlists
	levelDetail                      // browsing tracks inside a playlist
)

type playlistInputMode int

const (
	plInputNone playlistInputMode = iota
	plInputCreate
	plInputRename
	plInputFilter     // filter tracks in detail view
	plInputListFilter // filter playlists in list view
)

type playlistModel struct {
	store      *model.PlaylistStore
	level      playlistLevel
	listCur    int // cursor at list level
	listScroll int // scroll offset at list level
	detailCur  int // cursor at detail level
	detailScrl int // scroll offset at detail level
	width      int
	height     int

	// Visual select in detail level
	visual bool
	anchor int

	// Visual select in list level
	listVisual bool
	listAnchor int

	// Inline text input for create/rename/filter
	inputMode playlistInputMode
	input     textinput.Model

	// Filter state for detail view
	filterQuery    string
	filteredTracks []model.Track
	filteredIdx    []int // original indices in playlist

	// Filter state for list view
	listFilterQuery   string
	filteredPlaylists []*model.Playlist
	filteredPlIdx     []int // original indices in store.Playlists

	// Favorites set (track IDs) — set by App before rendering
	favSet map[string]bool
	// Tick counter for marquee animation — set by App before rendering
	tick int
	// Whether this panel is currently focused — set by App before rendering
	focused bool
	// Whether to show relative line numbers — set by App before rendering
	relNumbers bool

	// Radio mode — virtual track list overlaying detail view
	radioActive    bool
	radioSeedTitle string
	radioTracks    []model.Track
	radioSourceIdx int // index of playlist that seeded the radio (-1 if from search/queue)
	radioReturnCur int // cursor position to restore when leaving radio
}

func newPlaylistModel(store *model.PlaylistStore) playlistModel {
	ti := textinput.New()
	ti.CharLimit = 80
	return playlistModel{
		store: store,
		input: ti,
	}
}

// --- Filter ---

func (m *playlistModel) startFilter() tea.Cmd {
	if m.level == levelList {
		m.inputMode = plInputListFilter
		m.input.SetValue("")
		m.input.Placeholder = "Filter playlists..."
	} else {
		m.inputMode = plInputFilter
		m.input.SetValue("")
		m.input.Placeholder = "Filter tracks..."
	}
	return m.input.Focus()
}

// liveFilter updates the filtered results based on current input value.
// Called on every keystroke while typing in the filter input.
func (m *playlistModel) liveFilter() {
	query := strings.TrimSpace(m.input.Value())
	m.filterQuery = query

	p := m.currentPlaylist()
	if p == nil || query == "" {
		m.filteredTracks = nil
		m.filteredIdx = nil
		m.filterQuery = ""
		m.detailCur = 0
		return
	}

	m.filteredTracks = nil
	m.filteredIdx = nil
	for i, t := range p.Tracks {
		if fuzzyMatch(t.Title, t.Artist, query) {
			m.filteredTracks = append(m.filteredTracks, t)
			m.filteredIdx = append(m.filteredIdx, i)
		}
	}
	m.detailCur = 0
	m.visual = false
}

// confirmFilter locks in the filter and exits the input.
func (m *playlistModel) confirmFilter() {
	m.liveFilter()
	m.input.Blur()
	m.inputMode = plInputNone
}

func (m *playlistModel) clearFilter() {
	m.filterQuery = ""
	m.filteredTracks = nil
	m.filteredIdx = nil
}

func (m *playlistModel) isFiltered() bool {
	return m.filterQuery != ""
}

// --- List-level filter ---

func (m *playlistModel) liveListFilter() {
	query := strings.TrimSpace(m.input.Value())
	m.listFilterQuery = query

	if query == "" {
		m.filteredPlaylists = nil
		m.filteredPlIdx = nil
		m.listFilterQuery = ""
		m.listCur = 0
		return
	}

	m.filteredPlaylists = nil
	m.filteredPlIdx = nil
	for i, p := range m.store.Playlists {
		if fuzzyMatch(p.Name, "", query) {
			m.filteredPlaylists = append(m.filteredPlaylists, p)
			m.filteredPlIdx = append(m.filteredPlIdx, i)
		}
	}
	m.listCur = 0
}

func (m *playlistModel) confirmListFilter() {
	m.liveListFilter()
	m.input.Blur()
	m.inputMode = plInputNone
}

func (m *playlistModel) clearListFilter() {
	m.listFilterQuery = ""
	m.filteredPlaylists = nil
	m.filteredPlIdx = nil
}

func (m *playlistModel) isListFiltered() bool {
	return m.listFilterQuery != ""
}

// visiblePlaylists returns the playlist list to display — filtered or full.
func (m *playlistModel) visiblePlaylists() []*model.Playlist {
	if m.isListFiltered() {
		return m.filteredPlaylists
	}
	return m.store.Playlists
}

// visibleTracks returns the track list to display — filtered or full.
func (m *playlistModel) visibleTracks() []model.Track {
	if m.radioActive {
		return m.radioTracks
	}
	if m.isFiltered() {
		return m.filteredTracks
	}
	p := m.currentPlaylist()
	if p == nil {
		return nil
	}
	return p.Tracks
}

// --- Navigation ---

func (m *playlistModel) ensureVisible() {
	h := m.height
	if h < 1 {
		h = 10
	}
	switch m.level {
	case levelList:
		if m.listCur < m.listScroll {
			m.listScroll = m.listCur
		}
		if m.listCur >= m.listScroll+h {
			m.listScroll = m.listCur - h + 1
		}
		m.listScroll = max(m.listScroll, 0)
	case levelDetail:
		if m.detailCur < m.detailScrl {
			m.detailScrl = m.detailCur
		}
		if m.detailCur >= m.detailScrl+h {
			m.detailScrl = m.detailCur - h + 1
		}
		m.detailScrl = max(m.detailScrl, 0)
	}
}

func (m *playlistModel) moveUp() {
	switch m.level {
	case levelList:
		if m.listCur > 0 {
			m.listCur--
		}
	case levelDetail:
		if m.detailCur > 0 {
			m.detailCur--
		}
	}
	m.ensureVisible()
}

func (m *playlistModel) moveDown() {
	switch m.level {
	case levelList:
		total := m.totalListLen()
		if m.listCur < total-1 {
			m.listCur++
		}
	case levelDetail:
		tracks := m.visibleTracks()
		if m.detailCur < len(tracks)-1 {
			m.detailCur++
		}
	}
	m.ensureVisible()
}

func (m *playlistModel) goTop() {
	switch m.level {
	case levelList:
		m.listCur = 0
		m.listScroll = 0
	case levelDetail:
		m.detailCur = 0
		m.detailScrl = 0
	}
}

func (m *playlistModel) goBottom() {
	switch m.level {
	case levelList:
		total := m.totalListLen()
		if total > 0 {
			m.listCur = total - 1
		}
	case levelDetail:
		tracks := m.visibleTracks()
		if len(tracks) > 0 {
			m.detailCur = len(tracks) - 1
		}
	}
	m.ensureVisible()
}

func (m *playlistModel) halfPageDown(visibleHeight int) {
	half := max(visibleHeight/2, 1)
	switch m.level {
	case levelList:
		m.listCur += half
		total := m.totalListLen()
		m.listCur = min(m.listCur, total-1)
		m.listCur = max(m.listCur, 0)
	case levelDetail:
		m.detailCur += half
		tracks := m.visibleTracks()
		m.detailCur = min(m.detailCur, len(tracks)-1)
		m.detailCur = max(m.detailCur, 0)
	}
	m.ensureVisible()
}

func (m *playlistModel) halfPageUp(visibleHeight int) {
	half := max(visibleHeight/2, 1)
	switch m.level {
	case levelList:
		m.listCur -= half
		m.listCur = max(m.listCur, 0)
	case levelDetail:
		m.detailCur -= half
		m.detailCur = max(m.detailCur, 0)
	}
	m.ensureVisible()
}

func (m *playlistModel) enter() {
	if m.level == levelList {
		m.listVisual = false
		// Resolve actual store index for the playlist
		storeIdx := m.playlistIdx()
		if m.isListFiltered() {
			if m.listCur < 0 || m.listCur >= len(m.filteredPlIdx) {
				return
			}
			storeIdx = m.filteredPlIdx[m.listCur]
			m.listCur = storeIdx
			m.clearListFilter()
		}

		// If this playlist has a stashed radio, re-enter radio view
		if m.hasRadio() && storeIdx == m.radioSourceIdx {
			m.radioActive = true
			m.level = levelDetail
			m.detailCur = 0
			m.visual = false
			return
		}

		if m.currentPlaylist() == nil {
			return
		}
		m.level = levelDetail
		m.detailCur = 0
		m.detailScrl = 0
		m.visual = false
		m.clearFilter()
	}
}

// back navigates backwards. Returns true if at top level with nothing to do
// (signals the app to switch to previous view).
func (m *playlistModel) back() bool {
	switch m.level {
	case levelDetail:
		if m.radioActive {
			m.leaveRadio()
			return false
		}
		if m.isFiltered() {
			m.clearFilter()
			m.detailCur = 0
			return false
		}
		m.level = levelList
		m.visual = false
		return false
	case levelList:
		if m.isListFiltered() {
			m.clearListFilter()
			m.listCur = 0
			return false
		}
		// Nothing left to do — signal to go back to previous view
		return true
	}
	return false
}

// leaveRadio exits radio view but keeps the radio data so it can be re-entered.
// If radio was started from a playlist, return to that playlist's detail view
// with the cursor on the song that was selected when radio was started.
func (m *playlistModel) leaveRadio() {
	m.radioActive = false
	m.visual = false

	if m.radioSourceIdx >= 0 && m.radioSourceIdx < len(m.store.Playlists) {
		m.listCur = m.radioSourceIdx
		m.level = levelDetail
		m.detailCur = m.radioReturnCur
		// Clamp to valid range
		p := m.store.Playlists[m.radioSourceIdx]
		m.detailCur = min(m.detailCur, len(p.Tracks)-1)
		m.detailCur = max(m.detailCur, 0)
	} else {
		m.level = levelList
	}
}

// dismissRadio fully clears radio data (e.g. when starting a new radio).
func (m *playlistModel) dismissRadio() {
	m.radioActive = false
	m.radioTracks = nil
	m.radioSeedTitle = ""
	m.radioSourceIdx = -1
	m.visual = false
}

// hasRadio returns true if there's a stashed radio mix (even if not currently viewing it).
func (m *playlistModel) hasRadio() bool {
	return len(m.radioTracks) > 0
}

// playlistIdx returns the store.Playlists index for the current cursor.
func (m *playlistModel) playlistIdx() int {
	return m.listCur
}

// totalListLen returns the total number of playlists.
func (m *playlistModel) totalListLen() int {
	return len(m.store.Playlists)
}

func (m *playlistModel) currentPlaylist() *model.Playlist {
	idx := m.playlistIdx()
	if idx < 0 || idx >= len(m.store.Playlists) {
		return nil
	}
	return m.store.Playlists[idx]
}

// --- CRUD ---

func (m *playlistModel) startCreate() tea.Cmd {
	m.inputMode = plInputCreate
	m.input.SetValue("")
	m.input.Placeholder = "Playlist name"
	return m.input.Focus()
}

func (m *playlistModel) startRename() tea.Cmd {
	p := m.currentPlaylist()
	if p == nil {
		return nil
	}
	m.inputMode = plInputRename
	m.input.SetValue(p.Name)
	m.input.Placeholder = "New name"
	return m.input.Focus()
}

func (m *playlistModel) confirmInput() {
	val := strings.TrimSpace(m.input.Value())
	if val == "" {
		m.cancelInput()
		return
	}
	switch m.inputMode {
	case plInputCreate:
		_, _ = m.store.Create(val)
		m.listCur = len(m.store.Playlists) - 1 // move to newly created playlist
	case plInputRename:
		if p := m.currentPlaylist(); p != nil {
			_ = p.Rename(val)
		}
	}
	m.cancelInput()
}

func (m *playlistModel) cancelInput() {
	m.inputMode = plInputNone
	m.input.Blur()
}

func (m *playlistModel) deleteAtCursor() {
	switch m.level {
	case levelList:
		plIdx := m.playlistIdx()
		if plIdx >= 0 && plIdx < len(m.store.Playlists) {
			_ = m.store.Delete(plIdx)
			if m.isListFiltered() {
				m.liveListFilter()
			}
			total := m.totalListLen()
			if m.listCur >= total && m.listCur > 0 {
				m.listCur--
			}
		}
	case levelDetail:
		// Radio mode: remove from radioTracks directly
		if m.radioActive {
			if m.detailCur >= 0 && m.detailCur < len(m.radioTracks) {
				m.radioTracks = append(m.radioTracks[:m.detailCur], m.radioTracks[m.detailCur+1:]...)
				if m.detailCur >= len(m.radioTracks) && m.detailCur > 0 {
					m.detailCur--
				}
			}
			return
		}

		p := m.currentPlaylist()
		if p == nil {
			return
		}
		tracks := m.visibleTracks()
		if len(tracks) == 0 || m.detailCur >= len(tracks) {
			return
		}
		// Map filtered index to real index
		realIdx := m.detailCur
		if m.isFiltered() && m.detailCur < len(m.filteredIdx) {
			realIdx = m.filteredIdx[m.detailCur]
		}
		_ = p.RemoveTrack(realIdx)
		// Re-apply filter after removal
		if m.isFiltered() {
			m.liveFilter()
		}
		if m.detailCur >= len(m.visibleTracks()) && m.detailCur > 0 {
			m.detailCur--
		}
	}
}

// --- Visual select + yank ---

func (m *playlistModel) toggleVisual() {
	if m.level == levelDetail {
		m.visual = !m.visual
		if m.visual {
			m.anchor = m.detailCur
		}
	} else {
		m.listVisual = !m.listVisual
		if m.listVisual {
			m.listAnchor = m.listCur
		}
	}
}

func (m *playlistModel) swapVisualEnd() {
	if m.level == levelDetail && m.visual {
		m.anchor, m.detailCur = m.detailCur, m.anchor
	} else if m.level == levelList && m.listVisual {
		m.listAnchor, m.listCur = m.listCur, m.listAnchor
	}
}

func (m *playlistModel) isSelected(i int) bool {
	if m.level == levelDetail {
		if !m.visual {
			return false
		}
		lo, hi := m.anchor, m.detailCur
		if lo > hi {
			lo, hi = hi, lo
		}
		return i >= lo && i <= hi
	}
	// List level
	if !m.listVisual {
		return false
	}
	lo, hi := m.listAnchor, m.listCur
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

func (m *playlistModel) isListSelected(i int) bool {
	if !m.listVisual {
		return false
	}
	lo, hi := m.listAnchor, m.listCur
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

func (m *playlistModel) yankSelected() []model.Track {
	visible := m.visibleTracks()
	if len(visible) == 0 {
		return nil
	}
	var tracks []model.Track
	if m.visual {
		lo, hi := m.anchor, m.detailCur
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi && i < len(visible); i++ {
			tracks = append(tracks, visible[i])
		}
	} else if m.detailCur >= 0 && m.detailCur < len(visible) {
		tracks = append(tracks, visible[m.detailCur])
	}
	m.visual = false
	return tracks
}

// deleteVisual removes all tracks in the visual selection.
func (m *playlistModel) deleteVisual() {
	if !m.visual || m.level != levelDetail {
		m.visual = false
		return
	}
	lo, hi := m.anchor, m.detailCur
	if lo > hi {
		lo, hi = hi, lo
	}

	// Radio mode: remove from radioTracks directly
	if m.radioActive {
		hi = min(hi, len(m.radioTracks)-1)
		lo = max(lo, 0)
		m.radioTracks = append(m.radioTracks[:lo], m.radioTracks[hi+1:]...)
		m.visual = false
		m.detailCur = lo
		if m.detailCur >= len(m.radioTracks) && m.detailCur > 0 {
			m.detailCur = len(m.radioTracks) - 1
		}
		return
	}

	p := m.currentPlaylist()
	if p == nil {
		m.visual = false
		return
	}
	visible := m.visibleTracks()
	// Remove from high to low to keep indices valid
	for i := hi; i >= lo && i < len(visible); i-- {
		realIdx := i
		if m.isFiltered() && i < len(m.filteredIdx) {
			realIdx = m.filteredIdx[i]
		}
		_ = p.RemoveTrack(realIdx)
	}
	m.visual = false
	if m.isFiltered() {
		m.liveFilter()
	}
	m.detailCur = lo
	if m.detailCur >= len(m.visibleTracks()) && m.detailCur > 0 {
		m.detailCur = len(m.visibleTracks()) - 1
	}
}

// cutVisual removes visual selection and returns the cut tracks.
func (m *playlistModel) cutVisual() []model.Track {
	if !m.visual || m.level != levelDetail {
		m.visual = false
		return nil
	}
	visible := m.visibleTracks()
	lo, hi := m.anchor, m.detailCur
	if lo > hi {
		lo, hi = hi, lo
	}
	var tracks []model.Track
	for i := lo; i <= hi && i < len(visible); i++ {
		tracks = append(tracks, visible[i])
	}
	m.deleteVisual()
	return tracks
}

// cutAtCursor removes the track at cursor and returns it.
func (m *playlistModel) cutAtCursor() []model.Track {
	if m.level != levelDetail {
		return nil
	}
	visible := m.visibleTracks()
	if len(visible) == 0 || m.detailCur >= len(visible) {
		return nil
	}
	tracks := []model.Track{visible[m.detailCur]}
	m.deleteAtCursor()
	return tracks
}

// deleteListVisual removes all playlists in the visual selection at list level.
func (m *playlistModel) deleteListVisual() {
	if !m.listVisual || m.level != levelList {
		m.listVisual = false
		return
	}
	lo, hi := m.listAnchor, m.listCur
	if lo > hi {
		lo, hi = hi, lo
	}

	if m.isListFiltered() {
		// Map visible indices to real store indices, remove from high to low
		for i := hi; i >= lo; i-- {
			if i < len(m.filteredPlIdx) {
				_ = m.store.Delete(m.filteredPlIdx[i])
			}
		}
		m.liveListFilter()
	} else {
		for i := hi; i >= lo; i-- {
			if i < len(m.store.Playlists) {
				_ = m.store.Delete(i)
			}
		}
	}

	m.listVisual = false
	m.listCur = lo
	total := len(m.visiblePlaylists())
	if m.listCur >= total && m.listCur > 0 {
		m.listCur = total - 1
	}
}

// pasteAfterCursor inserts tracks after the current cursor position.
func (m *playlistModel) pasteAfterCursor(tracks []model.Track) {
	if m.level != levelDetail || len(tracks) == 0 {
		return
	}

	// Radio mode: insert into radioTracks
	if m.radioActive {
		insertIdx := min(m.detailCur+1, len(m.radioTracks))
		newTracks := make([]model.Track, 0, len(m.radioTracks)+len(tracks))
		newTracks = append(newTracks, m.radioTracks[:insertIdx]...)
		newTracks = append(newTracks, tracks...)
		newTracks = append(newTracks, m.radioTracks[insertIdx:]...)
		m.radioTracks = newTracks
		m.detailCur = insertIdx
		m.visual = false
		return
	}

	p := m.currentPlaylist()
	if p == nil {
		return
	}
	insertIdx := m.detailCur + 1
	if m.isFiltered() && m.detailCur < len(m.filteredIdx) {
		insertIdx = m.filteredIdx[m.detailCur] + 1
	}
	if insertIdx > len(p.Tracks) {
		insertIdx = len(p.Tracks)
	}
	// Insert tracks at position
	newTracks := make([]model.Track, 0, len(p.Tracks)+len(tracks))
	newTracks = append(newTracks, p.Tracks[:insertIdx]...)
	newTracks = append(newTracks, tracks...)
	newTracks = append(newTracks, p.Tracks[insertIdx:]...)
	p.Tracks = newTracks
	_ = p.Save()
	if m.isFiltered() {
		m.liveFilter()
	}
	m.detailCur = insertIdx
	m.visual = false
}

// --- Rendering ---

var (
	plNormalStyle = lipgloss.NewStyle().Padding(0, 2)
	plCursorStyle = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	plSelStyle    = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238"))
	plBothStyle   = lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("238")).Bold(true)
	plCountStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

func (m playlistModel) View() string {
	return m.ViewConstrained(m.width, m.height)
}

func (m playlistModel) ViewConstrained(width, height int) string {
	var b strings.Builder

	// Inline input takes 2 lines (label + blank) — but filter is rendered in the bottom bar
	inputLines := 0
	if m.inputMode == plInputCreate || m.inputMode == plInputRename {
		label := "Create playlist"
		if m.inputMode == plInputRename {
			label = "Rename playlist"
		}
		fmt.Fprintf(&b, "  %s: %s\n\n", label, m.input.View())
		inputLines = 2
	}

	switch m.level {
	case levelList:
		b.WriteString(m.viewListConstrained(inputLines, width, height))
	case levelDetail:
		b.WriteString(m.viewDetailConstrained(inputLines, width, height))
	}

	return b.String()
}

// isFilterActive returns true if a filter input is currently being typed.
func (m playlistModel) isFilterActive() bool {
	return m.inputMode == plInputFilter || m.inputMode == plInputListFilter
}

func (m playlistModel) viewListConstrained(extraLines, width, height int) string {
	var b strings.Builder

	pls := m.visiblePlaylists()

	if m.isListFiltered() {
		b.WriteString(plFilterStyle.Render(fmt.Sprintf("  (filter: %s)", m.listFilterQuery)))
		b.WriteString("\n\n")
	}

	// Build playlist list
	type listItem struct {
		name     string
		count    string
		radioTag string
	}
	var items []listItem

	for i, p := range pls {
		realIdx := i
		if m.isListFiltered() && i < len(m.filteredPlIdx) {
			realIdx = m.filteredPlIdx[i]
		}
		radioTag := ""
		if m.radioActive && realIdx == m.radioSourceIdx {
			radioTag = " " + plRadioStyle.Render("[radio]")
		}
		items = append(items, listItem{
			name:     p.Name,
			count:    plCountStyle.Render(fmt.Sprintf("(%d tracks)", len(p.Tracks))),
			radioTag: radioTag,
		})
	}

	if len(items) == 0 {
		b.WriteString("  No playlists. Press o to create one.")
		return b.String()
	}

	maxVisible := height - extraLines
	if m.isListFiltered() {
		maxVisible -= 2
	}
	if maxVisible < 1 {
		maxVisible = 10
	}

	// Adjust scroll to keep cursor visible
	if m.listCur < m.listScroll {
		m.listScroll = m.listCur
	}
	if m.listCur >= m.listScroll+maxVisible {
		m.listScroll = m.listCur - maxVisible + 1
	}
	m.listScroll = min(m.listScroll, len(items)-maxVisible)
	m.listScroll = max(m.listScroll, 0)
	start := m.listScroll
	end := min(start+maxVisible, len(items))

	for i := start; i < end; i++ {
		item := items[i]
		lineNum := i + 1
		if m.relNumbers && i != m.listCur {
			dist := i - m.listCur
			if dist < 0 {
				dist = -dist
			}
			lineNum = dist
		}
		line := fmt.Sprintf("%2d  %s  %s%s", lineNum, item.name, item.count, item.radioTag)

		isCursor := i == m.listCur && m.focused
		isSel := m.isListSelected(i) && m.focused
		style := plNormalStyle
		prefix := "  "
		switch {
		case isCursor && isSel:
			style = plBothStyle
			prefix = "> "
		case isCursor:
			style = plCursorStyle
			prefix = "> "
		case isSel:
			style = plSelStyle
		}
		rendered := style.Render(prefix + line)
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

var (
	plFilterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	plRadioStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

func (m playlistModel) viewDetailConstrained(extraLines, width, height int) string {
	var b strings.Builder

	// Label is now shown in the panel border, so only show filter indicator here
	headerLines := 0
	if m.isFiltered() {
		b.WriteString(plFilterStyle.Render(fmt.Sprintf("  (filter: %s)", m.filterQuery)))
		b.WriteString("\n\n")
		headerLines = 2
	}

	p := m.currentPlaylist()
	if p == nil && !m.radioActive {
		return ""
	}

	tracks := m.visibleTracks()
	if len(tracks) == 0 {
		if m.isFiltered() {
			b.WriteString("  No matching tracks.")
		} else {
			b.WriteString("  No tracks in this playlist.")
		}
		return b.String()
	}

	maxVisible := height - headerLines - extraLines
	if maxVisible < 1 {
		maxVisible = 10
	}

	// Adjust scroll to keep cursor visible
	if m.detailCur < m.detailScrl {
		m.detailScrl = m.detailCur
	}
	if m.detailCur >= m.detailScrl+maxVisible {
		m.detailScrl = m.detailCur - maxVisible + 1
	}
	m.detailScrl = min(m.detailScrl, len(tracks)-maxVisible)
	m.detailScrl = max(m.detailScrl, 0)
	start := m.detailScrl
	end := min(start+maxVisible, len(tracks))

	for i := start; i < end; i++ {
		t := tracks[i]
		dur := formatDuration(t.Duration)
		heart := ""
		if m.favSet[t.ID] {
			heart = favStyle.Render(" <3")
		}
		lineNum := i + 1
		if m.relNumbers && i != m.detailCur {
			dist := i - m.detailCur
			if dist < 0 {
				dist = -dist
			}
			lineNum = dist
		}
		isCursor := i == m.detailCur && m.focused
		isSel := m.isSelected(i) && m.focused

		style := plNormalStyle
		prefix := "  "
		switch {
		case isCursor && isSel:
			style = plBothStyle
			prefix = "> "
		case isCursor:
			style = plCursorStyle
			prefix = "> "
		case isSel:
			style = plSelStyle
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

// --- Add-to-playlist overlay ---

type overlayModel struct {
	active    bool
	playlists []*model.Playlist
	store     *model.PlaylistStore // for creating new playlists
	cursor    int
	tracks    []model.Track // tracks to add when confirmed
	selected  map[int]bool  // set of selected playlist indices (for multi-select via Space)
	visual    bool          // visual select mode
	anchor    int           // anchor for visual select
	// Inline create playlist
	creating bool
	input    textinput.Model
}

func newOverlayModel() overlayModel {
	ti := textinput.New()
	ti.CharLimit = 80
	return overlayModel{input: ti}
}

func (o *overlayModel) open(store *model.PlaylistStore, tracks []model.Track) {
	o.active = true
	o.store = store
	o.playlists = store.Playlists
	o.tracks = tracks
	o.cursor = 0
	o.selected = make(map[int]bool)
	o.creating = false
	o.input.Blur()
}

func (o *overlayModel) close() {
	o.active = false
	o.tracks = nil
	o.creating = false
	o.visual = false
	o.selected = make(map[int]bool)
	o.input.Blur()
}

func (o *overlayModel) moveUp() {
	if o.cursor > 0 {
		o.cursor--
	}
}

func (o *overlayModel) moveDown() {
	total := 1 + len(o.playlists) + 1 // Queue + playlists + "Create new"
	if o.cursor < total-1 {
		o.cursor++
	}
}

func (o *overlayModel) total() int {
	return 1 + len(o.playlists) + 1 // Queue + playlists + "Create new"
}

func (o *overlayModel) halfDown() {
	t := o.total()
	o.cursor += t / 2
	o.cursor = min(o.cursor, t-1)
}

func (o *overlayModel) halfUp() {
	t := o.total()
	o.cursor -= t / 2
	o.cursor = max(o.cursor, 0)
}

func (o *overlayModel) goTop() {
	o.cursor = 0
}

func (o *overlayModel) goBottom() {
	o.cursor = o.total() - 1
}

// isCreateSelected returns true if "Create new playlist" is the selected option (last).
func (o *overlayModel) isCreateSelected() bool {
	return o.cursor == 1+len(o.playlists)
}

// startCreate enters inline create mode.
func (o *overlayModel) startCreate() tea.Cmd {
	o.creating = true
	o.input.SetValue("")
	o.input.Placeholder = "Playlist name"
	return o.input.Focus()
}

// confirmCreate creates a new playlist, adds the tracks, and returns its name.
func (o *overlayModel) confirmCreate() (string, bool) {
	name := strings.TrimSpace(o.input.Value())
	if name == "" || o.store == nil {
		o.creating = false
		o.input.Blur()
		return "", false
	}
	p, err := o.store.Create(name)
	if err != nil {
		o.creating = false
		o.input.Blur()
		return "", false
	}
	_ = p.AddTracks(o.tracks...)
	o.creating = false
	o.input.Blur()
	return name, true
}

// isQueueSelected returns true if "Queue" is the selected option (cursor=0).
func (o *overlayModel) isQueueSelected() bool {
	return o.cursor == 0
}

// confirmPlaylist adds tracks to the selected playlist. Returns the playlist name.
func (o *overlayModel) confirmPlaylist() (string, bool) {
	plIdx := o.cursor - 1 // offset for Queue option at 0
	if plIdx < 0 || plIdx >= len(o.playlists) || len(o.tracks) == 0 {
		return "", false
	}
	p := o.playlists[plIdx]
	_ = p.AddTracks(o.tracks...)
	name := p.Name
	o.close()
	return name, true
}

var (
	overlayBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("75")).
				Padding(1, 2)
	overlayTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	overlayCurStyle   = lipgloss.NewStyle().Background(lipgloss.Color("238"))
)

var (
	overlayQueueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("48")).Bold(true)
	overlayCreateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
)

func (o overlayModel) View(width int) string {
	var b strings.Builder

	// Title with track count
	trackWord := "track"
	if len(o.tracks) != 1 {
		trackWord = "tracks"
	}
	title := fmt.Sprintf("Add %d %s to...", len(o.tracks), trackWord)
	b.WriteString(overlayTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Queue option (always first)
	queueCheck := "[ ] "
	if o.selected[0] {
		queueCheck = "[x] "
	}
	isVisual := func(idx int) bool {
		if !o.visual {
			return false
		}
		lo, hi := o.anchor, o.cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		return idx >= lo && idx <= hi
	}

	queueLine := "Queue"
	if o.cursor == 0 {
		b.WriteString(overlayCurStyle.Render("> " + queueCheck + queueLine))
	} else if isVisual(0) {
		b.WriteString(overlayCurStyle.Render("  " + queueCheck + queueLine))
	} else {
		b.WriteString("  " + queueCheck + overlayQueueStyle.Render(queueLine))
	}
	b.WriteString("\n")

	// Playlist options
	for i, p := range o.playlists {
		check := "[ ] "
		if o.selected[i+1] {
			check = "[x] "
		}
		line := fmt.Sprintf("%s%s (%d tracks)", check, p.Name, len(p.Tracks))
		if i+1 == o.cursor {
			b.WriteString(overlayCurStyle.Render("> " + line))
		} else if isVisual(i + 1) {
			b.WriteString(overlayCurStyle.Render("  " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	// Create new playlist option (always last)
	createIdx := 1 + len(o.playlists)
	if o.creating {
		b.WriteString("  " + overlayCreateStyle.Render("+ ") + o.input.View())
	} else {
		createLine := overlayCreateStyle.Render("+ Create new playlist")
		if o.cursor == createIdx {
			b.WriteString(overlayCurStyle.Render("> " + createLine))
		} else {
			b.WriteString("  " + createLine)
		}
	}
	b.WriteString("\n")

	// Footer hint
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	b.WriteString(helpStyle.Render("  Space=select  v=visual  Enter=confirm  Esc=cancel"))
	b.WriteString("\n")

	overlayWidth := max(width/2, 40)
	return overlayBorderStyle.Width(overlayWidth).Render(b.String())
}
