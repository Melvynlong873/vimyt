package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sadoaz/vimyt/internal/model"
	"github.com/Sadoaz/vimyt/internal/tui"
)

func main() {
	plStore, err := model.NewPlaylistStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading playlists: %v\n", err)
		os.Exit(1)
	}

	app := tui.New(plStore)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running vimyt: %v\n", err)
		os.Exit(1)
	}
}
