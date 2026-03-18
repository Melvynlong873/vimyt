package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxRadioHistory = 100

// RadioHistoryEntry records a single radio mix session.
type RadioHistoryEntry struct {
	SeedTitle  string    `json:"seed_title"`
	SeedArtist string    `json:"seed_artist"`
	TrackCount int       `json:"track_count"`
	StartedAt  time.Time `json:"started_at"`
	Tracks     []Track   `json:"tracks"` // full track list for recovery
}

// RadioHistory holds the list of radio mix sessions.
type RadioHistory struct {
	Entries []RadioHistoryEntry `json:"entries"`
	path    string
}

// LoadRadioHistory loads the radio history from disk.
func LoadRadioHistory() *RadioHistory {
	h := &RadioHistory{}
	p, err := radioHistoryPath()
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

// Add records a new radio mix entry and persists to disk.
func (h *RadioHistory) Add(seedTitle, seedArtist string, trackCount int, tracks []Track) {
	entry := RadioHistoryEntry{
		SeedTitle:  seedTitle,
		SeedArtist: seedArtist,
		TrackCount: trackCount,
		StartedAt:  time.Now(),
		Tracks:     tracks,
	}
	h.Entries = append(h.Entries, entry)
	// Trim to max
	if len(h.Entries) > maxRadioHistory {
		h.Entries = h.Entries[len(h.Entries)-maxRadioHistory:]
	}
	h.Save()
}

// Remove deletes the entry at the given index and persists to disk.
func (h *RadioHistory) Remove(idx int) {
	if idx < 0 || idx >= len(h.Entries) {
		return
	}
	h.Entries = append(h.Entries[:idx], h.Entries[idx+1:]...)
	h.Save()
}

// Save persists the radio history to disk.
func (h *RadioHistory) Save() {
	if h.path == "" {
		p, err := radioHistoryPath()
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

func radioHistoryPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "vimyt", "radio_history.json"), nil
}
