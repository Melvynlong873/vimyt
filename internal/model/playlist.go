package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Playlist represents a user-created local playlist.
type Playlist struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Tracks  []Track   `json:"tracks"`
	path    string    // filesystem path (not serialized)
}

// PlaylistStore manages loading and saving playlists from disk.
type PlaylistStore struct {
	Dir       string
	Playlists []*Playlist
}

// NewPlaylistStore creates a store and ensures the directory exists.
func NewPlaylistStore() (*PlaylistStore, error) {
	dir, err := playlistDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create playlist dir: %w", err)
	}
	s := &PlaylistStore{Dir: dir}
	if err := s.LoadAll(); err != nil {
		return nil, err
	}
	return s, nil
}

func playlistDir() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "vimyt", "playlists"), nil
}

// LoadAll reads all playlist JSON files from the store directory.
func (s *PlaylistStore) LoadAll() error {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return fmt.Errorf("read playlist dir: %w", err)
	}
	s.Playlists = nil
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p, err := loadPlaylist(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue // skip corrupt files
		}
		s.Playlists = append(s.Playlists, p)
	}
	return nil
}

func loadPlaylist(path string) (*Playlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Playlist
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.path = path
	return &p, nil
}

// Create makes a new playlist with the given name and persists it.
func (s *PlaylistStore) Create(name string) (*Playlist, error) {
	slug := slugify(name)
	path := filepath.Join(s.Dir, slug+".json")
	// Handle slug collision
	for i := 1; fileExists(path); i++ {
		path = filepath.Join(s.Dir, fmt.Sprintf("%s-%d.json", slug, i))
	}
	p := &Playlist{
		Name:    name,
		Created: time.Now(),
		path:    path,
	}
	if err := p.Save(); err != nil {
		return nil, err
	}
	s.Playlists = append(s.Playlists, p)
	return p, nil
}

// Delete removes a playlist from disk and from the store.
func (s *PlaylistStore) Delete(idx int) error {
	if idx < 0 || idx >= len(s.Playlists) {
		return nil
	}
	p := s.Playlists[idx]
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.Playlists = append(s.Playlists[:idx], s.Playlists[idx+1:]...)
	return nil
}

// Rename changes a playlist's name and updates the file.
func (p *Playlist) Rename(newName string) error {
	p.Name = newName
	return p.Save()
}

// AddTracks appends tracks and saves. Skips tracks already in the playlist (by ID).
func (p *Playlist) AddTracks(tracks ...Track) error {
	existing := make(map[string]bool, len(p.Tracks))
	for _, t := range p.Tracks {
		existing[t.ID] = true
	}
	for _, t := range tracks {
		if !existing[t.ID] {
			p.Tracks = append(p.Tracks, t)
			existing[t.ID] = true
		}
	}
	return p.Save()
}

// ContainsTrack returns true if the playlist contains a track with the given ID.
func (p *Playlist) ContainsTrack(id string) bool {
	for _, t := range p.Tracks {
		if t.ID == id {
			return true
		}
	}
	return false
}

// RemoveTrackByID removes the first track with the given ID and saves.
// Returns true if a track was removed.
func (p *Playlist) RemoveTrackByID(id string) bool {
	for i, t := range p.Tracks {
		if t.ID == id {
			p.Tracks = append(p.Tracks[:i], p.Tracks[i+1:]...)
			_ = p.Save()
			return true
		}
	}
	return false
}

// RemoveTrack removes the track at index i and saves.
func (p *Playlist) RemoveTrack(i int) error {
	if i < 0 || i >= len(p.Tracks) {
		return nil
	}
	p.Tracks = append(p.Tracks[:i], p.Tracks[i+1:]...)
	return p.Save()
}

// Save persists the playlist to disk.
func (p *Playlist) Save() error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "playlist"
	}
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Favorites returns the "Favorites" playlist, or nil if it doesn't exist.
func (s *PlaylistStore) Favorites() *Playlist {
	for _, p := range s.Playlists {
		if p.Name == "Favorites" {
			return p
		}
	}
	return nil
}

// FavoritesSet returns a set of track IDs that are in the Favorites playlist.
func (s *PlaylistStore) FavoritesSet() map[string]bool {
	fav := s.Favorites()
	if fav == nil {
		return nil
	}
	set := make(map[string]bool, len(fav.Tracks))
	for _, t := range fav.Tracks {
		set[t.ID] = true
	}
	return set
}

// SeedDefaults creates the Favorites playlist if the store is empty.
func (s *PlaylistStore) SeedDefaults() error {
	if len(s.Playlists) > 0 {
		return nil // already has playlists, don't overwrite
	}
	_, err := s.Create("Favorites")
	return err
}
