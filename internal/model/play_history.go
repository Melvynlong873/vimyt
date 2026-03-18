package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxPlayHistory = 500

// PlayHistoryEntry records a single played track.
type PlayHistoryEntry struct {
	TrackID  string        `json:"track_id"`
	Title    string        `json:"title"`
	Artist   string        `json:"artist"`
	Duration time.Duration `json:"duration"`
	PlayedAt time.Time     `json:"played_at"`
	Source   string        `json:"source"` // "search", "queue", "radio", "playlist:<name>"
}

// PlayHistory holds the list of played tracks.
type PlayHistory struct {
	Entries []PlayHistoryEntry `json:"entries"`
	path    string
}

// LoadPlayHistory loads the play history from disk.
func LoadPlayHistory() *PlayHistory {
	h := &PlayHistory{}
	p, err := playHistoryPath()
	if err != nil {
		return h
	}
	h.path = p
	data, err := os.ReadFile(p)
	if err != nil {
		return h
	}
	_ = json.Unmarshal(data, h)
	return h
}

// Add records a played track and persists to disk.
// Deduplicates consecutive plays of the same track.
func (h *PlayHistory) Add(t Track, source string) {
	// Don't add duplicates for the same track played back-to-back
	if len(h.Entries) > 0 {
		last := h.Entries[len(h.Entries)-1]
		if last.TrackID == t.ID && time.Since(last.PlayedAt) < 30*time.Second {
			return
		}
	}

	entry := PlayHistoryEntry{
		TrackID:  t.ID,
		Title:    t.Title,
		Artist:   t.Artist,
		Duration: t.Duration,
		PlayedAt: time.Now(),
		Source:   source,
	}
	h.Entries = append(h.Entries, entry)
	if len(h.Entries) > maxPlayHistory {
		h.Entries = h.Entries[len(h.Entries)-maxPlayHistory:]
	}
	h.Save()
}

// Tracks returns the history entries as Track slices (most recent first).
func (h *PlayHistory) Tracks() []Track {
	tracks := make([]Track, 0, len(h.Entries))
	for i := len(h.Entries) - 1; i >= 0; i-- {
		e := h.Entries[i]
		tracks = append(tracks, Track{
			ID:       e.TrackID,
			Title:    e.Title,
			Artist:   e.Artist,
			Duration: e.Duration,
		})
	}
	return tracks
}

// Len returns the number of history entries.
func (h *PlayHistory) Len() int {
	return len(h.Entries)
}

// Remove deletes the entry at the given index and persists to disk.
// The index is into the internal (chronological) Entries slice.
func (h *PlayHistory) Remove(idx int) {
	if idx < 0 || idx >= len(h.Entries) {
		return
	}
	h.Entries = append(h.Entries[:idx], h.Entries[idx+1:]...)
	h.Save()
}

// RemoveRange deletes entries from lo to hi (inclusive) and persists to disk.
// Indices are into the internal (chronological) Entries slice.
func (h *PlayHistory) RemoveRange(lo, hi int) {
	lo = max(lo, 0)
	hi = min(hi, len(h.Entries)-1)
	if lo > hi {
		return
	}
	h.Entries = append(h.Entries[:lo], h.Entries[hi+1:]...)
	h.Save()
}

// Save persists the play history to disk.
func (h *PlayHistory) Save() {
	if h.path == "" {
		p, err := playHistoryPath()
		if err != nil {
			return
		}
		h.path = p
	}
	dir := filepath.Dir(h.path)
	_ = os.MkdirAll(dir, 0o755)
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(h.path, data, 0o644)
}

func playHistoryPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "vimyt", "play_history.json"), nil
}
