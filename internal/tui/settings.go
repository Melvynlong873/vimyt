package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sadoaz/vimyt/internal/youtube"
)

// settingsOptions defines the available settings in order.
var settingsOptions = []struct {
	name string
	desc string
}{
	{"Autoplay", "Auto-advance to next track when current ends"},        // 0
	{"Shuffle", "Randomize next track selection"},                       // 1
	{"Focus Queue", "Auto-focus queue panel when playing a track"},      // 2
	{"Rel Numbers", "Show relative line numbers (vim-style)"},           // 3
	{"Pin Search", "Keep search panel expanded when unfocused"},         // 4
	{"Pin Playlist", "Keep playlist detail expanded when unfocused"},    // 5
	{"Pin Radio", "Keep radio history expanded when unfocused"},         // 6
	{"Show History", "Show play history panel below playlists"},         // 7
	{"Show Radio", "Show radio history panel below play history"},       // 8
	{"YT Auth", "Use browser cookies to access your private playlists"}, // 9
	{"Import", "Import playlist from YouTube URL"},                      // 10
}

// browserOptions is the cycle for the Auth Browser setting.
var browserOptions = []string{"", "firefox", "chrome", "chromium", "brave", "edge"}

func (a *App) settingValue(idx int) bool {
	switch idx {
	case 0:
		return a.autoplay
	case 1:
		return a.shuffle
	case 2:
		return a.autoFocusQueue
	case 3:
		return a.relNumbers
	case 4:
		return a.pinSearch
	case 5:
		return a.pinPlaylist
	case 6:
		return a.pinRadio
	case 7:
		return a.showHistory
	case 8:
		return a.showRadio
	case 9:
		return a.cookieBrowser != ""
	}
	return false
}

func (a *App) toggleSetting(idx int) {
	switch idx {
	case 0:
		a.autoplay = !a.autoplay
	case 1:
		a.shuffle = !a.shuffle
		if !a.shuffle {
			a.shufflePlayed = nil
		}
	case 2:
		a.autoFocusQueue = !a.autoFocusQueue
	case 3:
		a.relNumbers = !a.relNumbers
	case 4:
		a.pinSearch = !a.pinSearch
	case 5:
		a.pinPlaylist = !a.pinPlaylist
	case 6:
		a.pinRadio = !a.pinRadio
	case 7:
		a.showHistory = !a.showHistory
		if !a.showHistory && a.focusedPanel == panelHistory {
			a.focusedPanel = panelPlaylist
		}
	case 8:
		a.showRadio = !a.showRadio
		if !a.showRadio && a.focusedPanel == panelRadioHist {
			a.focusedPanel = panelPlaylist
		}
	case 9:
		// Cycle forward through browser options
		a.cycleBrowser(1)
	}
}

func (a *App) cycleBrowser(dir int) {
	cur := 0
	for i, b := range browserOptions {
		if b == a.cookieBrowser {
			cur = i
			break
		}
	}
	next := (cur + dir + len(browserOptions)) % len(browserOptions)
	a.cookieBrowser = browserOptions[next]
	youtube.SetCookieBrowser(a.cookieBrowser)
}

func (a App) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle import URL input mode
	if a.settingsImporting {
		switch {
		case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
			a.quit()
			return a, tea.Quit
		case key.Matches(msg, keys.Enter):
			url := strings.TrimSpace(a.settingsImportInput.Value())
			a.settingsImporting = false
			a.settingsImportInput.Blur()
			if url == "" {
				return a, nil
			}
			if a.importingPlaylist {
				return a, nil // already importing
			}
			a.importingPlaylist = true
			a.showSettings = false
			cmd := a.setStatus("Importing playlist...")
			importCmd := func() tea.Msg {
				name, tracks, err := youtube.FetchPlaylist(url)
				return importPlaylistMsg{name: name, tracks: tracks, err: err}
			}
			return a, tea.Batch(cmd, importCmd)
		case key.Matches(msg, keys.Escape):
			a.settingsImporting = false
			a.settingsImportInput.Blur()
			return a, nil
		default:
			var cmd tea.Cmd
			a.settingsImportInput, cmd = a.settingsImportInput.Update(msg)
			return a, cmd
		}
	}

	switch {
	case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
		a.quit()
		return a, tea.Quit
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Settings), msg.String() == "q":
		a.showSettings = false
		return a, nil
	case key.Matches(msg, keys.Up):
		if a.settingsCur > 0 {
			a.settingsCur--
		}
		return a, nil
	case key.Matches(msg, keys.Down):
		if a.settingsCur < len(settingsOptions)-1 {
			a.settingsCur++
		}
		return a, nil
	case key.Matches(msg, keys.HalfDown):
		a.settingsCur += len(settingsOptions) / 2
		a.settingsCur = min(a.settingsCur, len(settingsOptions)-1)
		return a, nil
	case key.Matches(msg, keys.HalfUp):
		a.settingsCur = max(a.settingsCur-len(settingsOptions)/2, 0)
		return a, nil
	case msg.String() == "g":
		a.settingsCur = 0
		return a, nil
	case msg.String() == "G":
		a.settingsCur = len(settingsOptions) - 1
		return a, nil
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Space):
		if a.settingsCur == 10 { // Import Playlist
			a.settingsImporting = true
			a.settingsImportInput.SetValue("")
			a.settingsImportInput.Focus()
			return a, nil
		}
		a.toggleSetting(a.settingsCur)
		return a, nil
	case msg.String() == "l", msg.String() == "right":
		if a.settingsCur == 9 { // Auth Browser — cycle forward
			a.cycleBrowser(1)
		}
		return a, nil
	case msg.String() == "h", msg.String() == "left":
		if a.settingsCur == 9 { // Auth Browser — cycle backward
			a.cycleBrowser(-1)
		}
		return a, nil
	}
	return a, nil
}

var (
	settingsOnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)
	settingsOffStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	settingsCurStyle = lipgloss.NewStyle().Background(lipgloss.Color("238"))
)

func (a App) renderSettings() string {
	var b strings.Builder
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	for i, opt := range settingsOptions {
		var toggle string
		switch i {
		case 9: // Auth Browser — show browser name
			if a.cookieBrowser == "" {
				toggle = settingsOffStyle.Render("[OFF]")
			} else {
				toggle = settingsOnStyle.Render(fmt.Sprintf("[%-9s]", a.cookieBrowser))
			}
		case 10: // Import Playlist — action, not a toggle
			toggle = actionStyle.Render("[>>>]")
		default:
			val := a.settingValue(i)
			if val {
				toggle = settingsOnStyle.Render("[ON] ")
			} else {
				toggle = settingsOffStyle.Render("[OFF]")
			}
		}
		line := fmt.Sprintf("  %s  %-14s %s", toggle, opt.name, descStyle.Render(opt.desc))
		if i == a.settingsCur {
			line = settingsCurStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	// Show import URL input if active
	if a.settingsImporting {
		b.WriteString("\n  " + a.settingsImportInput.View() + "\n")
		b.WriteString("\n  Enter = import  Esc = cancel")
	} else {
		b.WriteString("\n  j/k = navigate  ^d/^u = half-page  gg/G = top/bottom  Enter/Space = toggle  Esc/S/q = close")
	}

	boxW := max(a.width*2/3, 50)
	box := overlayBorderStyle.Width(boxW).Render(
		overlayTitleStyle.Render("Settings") + "\n\n" + b.String(),
	)
	return box
}
