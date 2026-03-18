package tui

import (
	"fmt"

	"github.com/Sadoaz/vimyt/internal/model"
)

type undoKind int

const (
	undoTracks           undoKind = iota // track-level mutation in a playlist
	undoPlaylistDel                      // entire playlist was deleted
	undoQueue                            // queue mutation
	undoMultiPlaylistDel                 // multiple playlists deleted at once
	undoPlaylistCreate                   // playlist was created (paste) — undo = delete it
	undoFavorite                         // favorite toggle — undoable from any panel
)

// undoEntry captures a snapshot before a mutation.
type undoEntry struct {
	kind        undoKind
	playlistIdx int           // index in store.Playlists
	tracks      []model.Track // snapshot of tracks before change
	cursor      int           // cursor position before change
	plName      string        // playlist name (for undoPlaylistDel)
	queueCur    int           // queue current playing index (for undoQueue)
	// For undoMultiPlaylistDel
	multiPl []struct {
		idx    int
		name   string
		tracks []model.Track
	}
}

// saveUndo snapshots the current playlist's tracks for undo.
func (a *App) saveUndo() {
	if a.focusedPanel != panelPlaylist || a.playlist.level != levelDetail {
		return
	}
	p := a.playlist.currentPlaylist()
	if p == nil {
		return
	}
	snap := make([]model.Track, len(p.Tracks))
	copy(snap, p.Tracks)
	a.undoStack = append(a.undoStack, undoEntry{
		kind:        undoTracks,
		playlistIdx: a.playlist.listCur,
		tracks:      snap,
		cursor:      a.playlist.detailCur,
	})
	// Clear redo stack on new change
	a.redoStack = nil
}

func (a *App) saveQueueUndo() {
	snap := make([]model.Track, len(a.qdata.Tracks))
	copy(snap, a.qdata.Tracks)
	a.undoStack = append(a.undoStack, undoEntry{
		kind:     undoQueue,
		tracks:   snap,
		cursor:   a.queue.cursor,
		queueCur: a.qdata.Current,
	})
	a.redoStack = nil
}

// saveUndoPlaylistDel snapshots an entire playlist before deletion.
func (a *App) saveUndoPlaylistDel() {
	if a.focusedPanel != panelPlaylist || a.playlist.level != levelList {
		return
	}
	pls := a.playlist.visiblePlaylists()
	if a.playlist.listCur < 0 || a.playlist.listCur >= len(pls) {
		return
	}
	realIdx := a.playlist.listCur
	if a.playlist.isListFiltered() && a.playlist.listCur < len(a.playlist.filteredPlIdx) {
		realIdx = a.playlist.filteredPlIdx[a.playlist.listCur]
	}
	p := a.playlist.store.Playlists[realIdx]
	snap := make([]model.Track, len(p.Tracks))
	copy(snap, p.Tracks)
	a.undoStack = append(a.undoStack, undoEntry{
		kind:        undoPlaylistDel,
		playlistIdx: realIdx,
		tracks:      snap,
		plName:      p.Name,
		cursor:      a.playlist.listCur,
	})
	a.redoStack = nil
}

func (a *App) isUndoForPanel(entry undoEntry, p panel) bool {
	switch entry.kind {
	case undoQueue:
		return p == panelQueue
	case undoTracks, undoPlaylistDel, undoMultiPlaylistDel, undoPlaylistCreate:
		return p == panelPlaylist
	case undoFavorite:
		return true // favorite toggle is reachable from any panel
	}
	return true // unknown kind — allow from any panel
}

func (a *App) performUndoForPanel(p panel) string {
	// Find the most recent undo entry matching the panel
	for i := len(a.undoStack) - 1; i >= 0; i-- {
		if a.isUndoForPanel(a.undoStack[i], p) {
			// Move this entry to the front (remove from position i)
			entry := a.undoStack[i]
			a.undoStack = append(a.undoStack[:i], a.undoStack[i+1:]...)
			a.undoStack = append(a.undoStack, entry)
			return a.performUndo()
		}
	}
	return "Nothing to undo"
}

func (a *App) performRedoForPanel(p panel) string {
	for i := len(a.redoStack) - 1; i >= 0; i-- {
		if a.isUndoForPanel(a.redoStack[i], p) {
			entry := a.redoStack[i]
			a.redoStack = append(a.redoStack[:i], a.redoStack[i+1:]...)
			a.redoStack = append(a.redoStack, entry)
			return a.performRedo()
		}
	}
	return "Nothing to redo"
}

func (a *App) performUndo() string {
	if len(a.undoStack) == 0 {
		return "Nothing to undo"
	}
	entry := a.undoStack[len(a.undoStack)-1]
	a.undoStack = a.undoStack[:len(a.undoStack)-1]

	switch entry.kind {
	case undoQueue:
		// Save current queue state to redo
		redoSnap := make([]model.Track, len(a.qdata.Tracks))
		copy(redoSnap, a.qdata.Tracks)
		a.redoStack = append(a.redoStack, undoEntry{
			kind:     undoQueue,
			tracks:   redoSnap,
			cursor:   a.queue.cursor,
			queueCur: a.qdata.Current,
		})
		// Restore queue
		a.qdata.Tracks = entry.tracks
		a.qdata.Current = entry.queueCur
		a.queue.cursor = entry.cursor
		a.queue.clampCursor(a.qdata.Len())
		if a.queue.isFiltered() {
			a.queue.liveFilter(a.qdata)
		}
		return "Undo queue"

	case undoPlaylistDel:
		// Re-create the deleted playlist
		pl, err := a.playlist.store.Create(entry.plName)
		if err != nil {
			return "Undo failed"
		}
		pl.Tracks = entry.tracks
		_ = pl.Save()
		// Move the playlist from the end back to its original position
		pls := a.playlist.store.Playlists
		newIdx := len(pls) - 1
		targetIdx := max(entry.playlistIdx, 0)
		targetIdx = min(targetIdx, newIdx)
		if targetIdx < newIdx {
			item := pls[newIdx]
			copy(pls[targetIdx+1:], pls[targetIdx:newIdx])
			pls[targetIdx] = item
		}
		// Save redo entry (the deletion we just undid)
		a.redoStack = append(a.redoStack, undoEntry{
			kind:        undoPlaylistDel,
			playlistIdx: targetIdx,
			tracks:      entry.tracks,
			plName:      entry.plName,
			cursor:      entry.cursor,
		})
		a.playlist.listCur = entry.cursor
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return "Undo: restored " + entry.plName

	case undoTracks:
		if entry.playlistIdx >= len(a.playlist.store.Playlists) {
			return "Undo failed"
		}
		p := a.playlist.store.Playlists[entry.playlistIdx]
		// Save current state to redo
		redoSnap := make([]model.Track, len(p.Tracks))
		copy(redoSnap, p.Tracks)
		a.redoStack = append(a.redoStack, undoEntry{
			kind:        undoTracks,
			playlistIdx: entry.playlistIdx,
			tracks:      redoSnap,
			cursor:      a.playlist.detailCur,
		})
		// Restore
		p.Tracks = entry.tracks
		_ = p.Save()
		a.playlist.detailCur = entry.cursor
		if a.playlist.detailCur >= len(p.Tracks) && a.playlist.detailCur > 0 {
			a.playlist.detailCur = len(p.Tracks) - 1
		}
		if a.playlist.isFiltered() {
			a.playlist.liveFilter()
		}
		return "Undo"

	case undoFavorite:
		if entry.playlistIdx >= len(a.playlist.store.Playlists) {
			return "Undo failed"
		}
		p := a.playlist.store.Playlists[entry.playlistIdx]
		beforeLen := len(p.Tracks)
		redoSnap := make([]model.Track, len(p.Tracks))
		copy(redoSnap, p.Tracks)
		a.redoStack = append(a.redoStack, undoEntry{
			kind:        undoFavorite,
			playlistIdx: entry.playlistIdx,
			tracks:      redoSnap,
		})
		p.Tracks = entry.tracks
		_ = p.Save()
		return fmt.Sprintf("Undo favorite (%d → %d tracks)", beforeLen, len(p.Tracks))

	case undoMultiPlaylistDel:
		// Restore all deleted playlists in order (low index first)
		for _, mp := range entry.multiPl {
			pl, err := a.playlist.store.Create(mp.name)
			if err != nil {
				continue
			}
			pl.Tracks = mp.tracks
			_ = pl.Save()
			// Move to original position
			pls := a.playlist.store.Playlists
			newIdx := len(pls) - 1
			targetIdx := mp.idx
			targetIdx = min(targetIdx, newIdx)
			if targetIdx < newIdx {
				item := pls[newIdx]
				copy(pls[targetIdx+1:], pls[targetIdx:newIdx])
				pls[targetIdx] = item
			}
		}
		a.redoStack = append(a.redoStack, undoEntry{
			kind:    undoMultiPlaylistDel,
			multiPl: entry.multiPl,
			cursor:  entry.cursor,
		})
		a.playlist.listCur = entry.cursor
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return fmt.Sprintf("Undo: restored %d playlists", len(entry.multiPl))

	case undoPlaylistCreate:
		// Playlists were created (via paste) — undo by deleting them.
		// Delete from high index to low to avoid shifting issues.
		count := 0
		for i := len(entry.multiPl) - 1; i >= 0; i-- {
			idx := entry.multiPl[i].idx
			if idx < len(a.playlist.store.Playlists) {
				_ = a.playlist.store.Delete(idx)
				count++
			}
		}
		a.redoStack = append(a.redoStack, undoEntry{
			kind:    undoPlaylistCreate,
			multiPl: entry.multiPl,
			cursor:  entry.cursor,
		})
		a.playlist.listCur = entry.cursor
		a.playlist.listCur = min(a.playlist.listCur, max(len(a.playlist.store.Playlists)-1, 0))
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return fmt.Sprintf("Undo: deleted %d playlists", count)
	}
	return ""
}

func (a *App) performRedo() string {
	if len(a.redoStack) == 0 {
		return "Nothing to redo"
	}
	entry := a.redoStack[len(a.redoStack)-1]
	a.redoStack = a.redoStack[:len(a.redoStack)-1]

	switch entry.kind {
	case undoQueue:
		// Save current queue state to undo
		undoSnap := make([]model.Track, len(a.qdata.Tracks))
		copy(undoSnap, a.qdata.Tracks)
		a.undoStack = append(a.undoStack, undoEntry{
			kind:     undoQueue,
			tracks:   undoSnap,
			cursor:   a.queue.cursor,
			queueCur: a.qdata.Current,
		})
		// Restore queue
		a.qdata.Tracks = entry.tracks
		a.qdata.Current = entry.queueCur
		a.queue.cursor = entry.cursor
		a.queue.clampCursor(a.qdata.Len())
		if a.queue.isFiltered() {
			a.queue.liveFilter(a.qdata)
		}
		return "Redo queue"

	case undoPlaylistDel:
		// Re-delete the playlist
		if entry.playlistIdx >= len(a.playlist.store.Playlists) {
			return "Redo failed"
		}
		p := a.playlist.store.Playlists[entry.playlistIdx]
		snap := make([]model.Track, len(p.Tracks))
		copy(snap, p.Tracks)
		a.undoStack = append(a.undoStack, undoEntry{
			kind:        undoPlaylistDel,
			playlistIdx: entry.playlistIdx,
			tracks:      snap,
			plName:      p.Name,
			cursor:      a.playlist.listCur,
		})
		_ = a.playlist.store.Delete(entry.playlistIdx)
		if a.playlist.listCur >= len(a.playlist.store.Playlists) && a.playlist.listCur > 0 {
			a.playlist.listCur--
		}
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return "Redo: deleted " + p.Name

	case undoTracks:
		if entry.playlistIdx >= len(a.playlist.store.Playlists) {
			return "Redo failed"
		}
		p := a.playlist.store.Playlists[entry.playlistIdx]
		// Save current state to undo
		undoSnap := make([]model.Track, len(p.Tracks))
		copy(undoSnap, p.Tracks)
		a.undoStack = append(a.undoStack, undoEntry{
			kind:        undoTracks,
			playlistIdx: entry.playlistIdx,
			tracks:      undoSnap,
			cursor:      a.playlist.detailCur,
		})
		// Restore
		p.Tracks = entry.tracks
		_ = p.Save()
		a.playlist.detailCur = entry.cursor
		if a.playlist.detailCur >= len(p.Tracks) && a.playlist.detailCur > 0 {
			a.playlist.detailCur = len(p.Tracks) - 1
		}
		if a.playlist.isFiltered() {
			a.playlist.liveFilter()
		}
		return "Redo"

	case undoFavorite:
		if entry.playlistIdx >= len(a.playlist.store.Playlists) {
			return "Redo failed"
		}
		p := a.playlist.store.Playlists[entry.playlistIdx]
		undoSnap := make([]model.Track, len(p.Tracks))
		copy(undoSnap, p.Tracks)
		a.undoStack = append(a.undoStack, undoEntry{
			kind:        undoFavorite,
			playlistIdx: entry.playlistIdx,
			tracks:      undoSnap,
		})
		p.Tracks = entry.tracks
		_ = p.Save()
		return "Redo favorite"

	case undoMultiPlaylistDel:
		// Re-delete the playlists (high index first to avoid shifting)
		// First save current state for undo
		var savedPl []struct {
			idx    int
			name   string
			tracks []model.Track
		}
		for _, mp := range entry.multiPl {
			if mp.idx < len(a.playlist.store.Playlists) {
				p := a.playlist.store.Playlists[mp.idx]
				snap := make([]model.Track, len(p.Tracks))
				copy(snap, p.Tracks)
				savedPl = append(savedPl, struct {
					idx    int
					name   string
					tracks []model.Track
				}{idx: mp.idx, name: p.Name, tracks: snap})
			}
		}
		// Delete from high to low
		for i := len(entry.multiPl) - 1; i >= 0; i-- {
			idx := entry.multiPl[i].idx
			if idx < len(a.playlist.store.Playlists) {
				_ = a.playlist.store.Delete(idx)
			}
		}
		a.undoStack = append(a.undoStack, undoEntry{
			kind:    undoMultiPlaylistDel,
			multiPl: savedPl,
			cursor:  entry.cursor,
		})
		a.playlist.listCur = entry.cursor
		total := len(a.playlist.visiblePlaylists())
		if a.playlist.listCur >= total && a.playlist.listCur > 0 {
			a.playlist.listCur = total - 1
		}
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return fmt.Sprintf("Redo: deleted %d playlists", len(entry.multiPl))

	case undoPlaylistCreate:
		// Re-create the playlists that were deleted by undo.
		var newMultiPl []struct {
			idx    int
			name   string
			tracks []model.Track
		}
		for _, mp := range entry.multiPl {
			pl, err := a.playlist.store.Create(mp.name)
			if err != nil {
				continue
			}
			_ = pl.AddTracks(mp.tracks...)
			newMultiPl = append(newMultiPl, struct {
				idx    int
				name   string
				tracks []model.Track
			}{idx: len(a.playlist.store.Playlists) - 1, name: mp.name, tracks: mp.tracks})
		}
		a.undoStack = append(a.undoStack, undoEntry{
			kind:    undoPlaylistCreate,
			multiPl: newMultiPl,
			cursor:  entry.cursor,
		})
		a.playlist.listCur = len(a.playlist.store.Playlists) - 1
		if a.playlist.isListFiltered() {
			a.playlist.liveListFilter()
		}
		return fmt.Sprintf("Redo: created %d playlists", len(entry.multiPl))
	}
	return ""
}
