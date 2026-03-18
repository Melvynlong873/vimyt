// Package model defines the data types and persistence for vimyt.
package model

import "time"

// Track represents a single music track.
type Track struct {
	ID        string
	Title     string
	Artist    string
	Duration  time.Duration
	StreamURL string
}

// PlayerState represents the current playback state.
type PlayerState int

const (
	Stopped PlayerState = iota
	Playing
	Paused
)

// PlayerStatus holds the current player state for the TUI to display.
type PlayerStatus struct {
	State    PlayerState
	Track    *Track
	Position time.Duration
	Volume   int // 0-100
}
