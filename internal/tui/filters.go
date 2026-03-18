package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (a App) updatePlaylistInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
		a.quit()
		return a, tea.Quit
	case key.Matches(msg, keys.Enter):
		switch a.playlist.inputMode {
		case plInputFilter:
			a.playlist.confirmFilter()
		case plInputListFilter:
			a.playlist.confirmListFilter()
		default:
			a.playlist.confirmInput()
		}
		return a, nil
	case key.Matches(msg, keys.Escape):
		switch a.playlist.inputMode {
		case plInputFilter:
			a.playlist.clearFilter()
		case plInputListFilter:
			a.playlist.clearListFilter()
		}
		a.playlist.cancelInput()
		return a, nil
	default:
		var cmd tea.Cmd
		a.playlist.input, cmd = a.playlist.input.Update(msg)
		// Live filter: update results on every keystroke
		switch a.playlist.inputMode {
		case plInputFilter:
			a.playlist.liveFilter()
		case plInputListFilter:
			a.playlist.liveListFilter()
		}
		return a, cmd
	}
}

func (a App) updateQueueFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
		a.quit()
		return a, tea.Quit
	case key.Matches(msg, keys.Enter):
		a.queue.confirmFilter(a.qdata)
		return a, nil
	case key.Matches(msg, keys.Escape):
		a.queue.clearFilter()
		a.queue.filterInput.Blur()
		return a, nil
	default:
		var cmd tea.Cmd
		a.queue.filterInput, cmd = a.queue.filterInput.Update(msg)
		a.queue.liveFilter(a.qdata)
		return a, cmd
	}
}

func (a App) updateSearchFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
		a.quit()
		return a, tea.Quit
	case key.Matches(msg, keys.Enter):
		a.search.confirmFilter()
		return a, nil
	case key.Matches(msg, keys.Escape):
		a.search.clearFilter()
		a.search.filterInput.Blur()
		return a, nil
	default:
		var cmd tea.Cmd
		a.search.filterInput, cmd = a.search.filterInput.Update(msg)
		a.search.liveFilter()
		return a, cmd
	}
}

func (a App) updateHistoryFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
		a.quit()
		return a, tea.Quit
	case key.Matches(msg, keys.Enter):
		a.history.confirmFilter()
		return a, nil
	case key.Matches(msg, keys.Escape):
		a.history.clearFilter()
		a.history.filterInput.Blur()
		return a, nil
	default:
		var cmd tea.Cmd
		a.history.filterInput, cmd = a.history.filterInput.Update(msg)
		a.history.liveFilter()
		return a, cmd
	}
}
