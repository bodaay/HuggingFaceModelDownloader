// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/smartdl"
)

// SelectorResult contains the result of the interactive selection.
type SelectorResult struct {
	// Action is what the user chose: "download", "copy", "cancel"
	Action string

	// SelectedFilters is the list of filter values to use with -F flag.
	SelectedFilters []string

	// CLICommand is the generated CLI command.
	CLICommand string
}

// categoryGroup groups items by category for display.
type categoryGroup struct {
	Name  string
	Title string
	Items []itemState
}

// itemState tracks the selection state of an item.
type itemState struct {
	Item     smartdl.SelectableItem
	Selected bool
	Index    int // Global index for cursor tracking
}

// SelectorModel is the bubbletea model for interactive selection.
type SelectorModel struct {
	// Input data
	repoInfo *smartdl.RepoInfo

	// Grouped items
	categories []categoryGroup

	// All items flat (for index mapping)
	allItems []itemState

	// Navigation state
	cursor    int
	maxCursor int

	// Terminal dimensions
	width  int
	height int

	// Result
	result SelectorResult
	done   bool
}

// NewSelectorModel creates a new selector model from repo analysis.
func NewSelectorModel(info *smartdl.RepoInfo) *SelectorModel {
	m := &SelectorModel{
		repoInfo: info,
	}

	// Group items by category
	categoryMap := make(map[string][]smartdl.SelectableItem)
	categoryOrder := []string{}

	for _, item := range info.SelectableItems {
		cat := item.Category
		if cat == "" {
			cat = "options"
		}
		if _, exists := categoryMap[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		categoryMap[cat] = append(categoryMap[cat], item)
	}

	// Build category groups
	globalIdx := 0
	for _, catName := range categoryOrder {
		items := categoryMap[catName]
		group := categoryGroup{
			Name:  catName,
			Title: FormatCategoryTitle(catName),
		}

		for _, item := range items {
			state := itemState{
				Item:     item,
				Selected: item.Recommended, // Pre-select recommended items
				Index:    globalIdx,
			}
			group.Items = append(group.Items, state)
			m.allItems = append(m.allItems, state)
			globalIdx++
		}

		m.categories = append(m.categories, group)
	}

	m.maxCursor = len(m.allItems) - 1
	if m.maxCursor < 0 {
		m.maxCursor = 0
	}

	return m
}

// Init implements tea.Model.
func (m *SelectorModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *SelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.result.Action = "cancel"
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < m.maxCursor {
				m.cursor++
			}

		case " ": // Space to toggle
			m.toggleCurrent()

		case "a": // Select all
			m.selectAll(true)

		case "n": // Select none
			m.selectAll(false)

		case "enter": // Download
			m.result.Action = "download"
			m.result.SelectedFilters = m.getSelectedFilters()
			m.result.CLICommand = m.generateCommand()
			m.done = true
			return m, tea.Quit

		case "c": // Copy command
			cmd := m.generateCommand()
			if err := clipboard.WriteAll(cmd); err == nil {
				m.result.Action = "copy"
				m.result.CLICommand = cmd
				m.result.SelectedFilters = m.getSelectedFilters()
				m.done = true
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// View implements tea.Model.
func (m *SelectorModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder

	// Header
	title := TitleStyle.Render(m.repoInfo.Repo)
	typeInfo := HeaderInfoStyle.Render(fmt.Sprintf("Type: %s (%s)", m.repoInfo.Type, m.repoInfo.TypeDescription))
	statsInfo := SubtitleStyle.Render(fmt.Sprintf("%d files • %s total", m.repoInfo.FileCount, m.repoInfo.TotalSizeHuman))

	b.WriteString(title + "\n")
	b.WriteString(typeInfo + "\n")
	b.WriteString(statsInfo + "\n\n")

	// Items by category
	for _, cat := range m.categories {
		b.WriteString(CategoryStyle.Render(cat.Title) + "\n\n")

		for i, state := range cat.Items {
			// Find actual index in allItems
			actualIdx := 0
			for j, all := range m.allItems {
				if all.Index == state.Index {
					actualIdx = j
					break
				}
			}

			// Update state from allItems (which has the actual selection state)
			state = m.allItems[actualIdx]

			// Cursor indicator
			cursor := "  "
			if m.cursor == actualIdx {
				cursor = CursorStyle.Render("> ")
			}

			// Checkbox
			checkbox := RenderCheckbox(state.Selected)

			// Label
			label := state.Item.Label
			if state.Item.Recommended {
				label = label + " " + RecommendedBadge.String()
			}

			// Size
			sizeStr := ""
			if state.Item.SizeHuman != "" {
				sizeStr = SizeLabelStyle.Render(state.Item.SizeHuman)
			}

			// Quality stars
			stars := ""
			if state.Item.Quality > 0 {
				stars = " " + RenderStars(state.Item.Quality)
			}

			// RAM estimate (for GGUF)
			ramStr := ""
			if state.Item.RAMHuman != "" {
				ramStr = RAMLabelStyle.Render("~" + state.Item.RAMHuman + " RAM")
			}

			// Build the line
			line := fmt.Sprintf("%s%s %s  %s%s  %s",
				cursor, checkbox, label, sizeStr, stars, ramStr)

			// Highlight current line
			if m.cursor == actualIdx {
				line = SelectedItemStyle.Render(line)
			} else {
				line = ItemStyle.Render(line)
			}

			b.WriteString(line + "\n")

			// Description on next line (indented)
			if state.Item.Description != "" && (m.cursor == actualIdx || i == 0) {
				desc := DescriptionStyle.Render("    " + state.Item.Description)
				b.WriteString(desc + "\n")
			}
		}
		b.WriteString("\n")
	}

	// Summary
	selectedCount, totalSize := m.getSelectionStats()
	summaryLine := SummaryLabelStyle.Render("Selected: ") +
		SummaryValueStyle.Render(fmt.Sprintf("%d items", selectedCount)) +
		SummaryLabelStyle.Render(" • ") +
		SummaryValueStyle.Render(humanSize(totalSize))
	b.WriteString(summaryLine + "\n")

	// Command preview
	cmd := m.generateCommand()
	cmdBox := CommandLabelStyle.Render("Command: ") + CommandTextStyle.Render(cmd)
	b.WriteString(CommandBoxStyle.Render(cmdBox) + "\n")

	// Footer with keybindings
	footer := m.renderFooter()
	b.WriteString(footer)

	return b.String()
}

// toggleCurrent toggles the selection of the current item.
func (m *SelectorModel) toggleCurrent() {
	if m.cursor >= 0 && m.cursor < len(m.allItems) {
		m.allItems[m.cursor].Selected = !m.allItems[m.cursor].Selected

		// Update category groups as well
		for i := range m.categories {
			for j := range m.categories[i].Items {
				if m.categories[i].Items[j].Index == m.allItems[m.cursor].Index {
					m.categories[i].Items[j].Selected = m.allItems[m.cursor].Selected
				}
			}
		}
	}
}

// selectAll selects or deselects all items.
func (m *SelectorModel) selectAll(selected bool) {
	for i := range m.allItems {
		m.allItems[i].Selected = selected
	}
	for i := range m.categories {
		for j := range m.categories[i].Items {
			m.categories[i].Items[j].Selected = selected
		}
	}
}

// getSelectedFilters returns the filter values of selected items.
func (m *SelectorModel) getSelectedFilters() []string {
	var filters []string
	for _, item := range m.allItems {
		if item.Selected && item.Item.FilterValue != "" {
			filters = append(filters, item.Item.FilterValue)
		}
	}
	return filters
}

// getSelectionStats returns the count and total size of selected items.
func (m *SelectorModel) getSelectionStats() (count int, size int64) {
	for _, item := range m.allItems {
		if item.Selected {
			count++
			size += item.Item.Size
		}
	}
	return
}

// generateCommand generates the CLI command for current selection.
func (m *SelectorModel) generateCommand() string {
	filters := m.getSelectedFilters()
	return m.repoInfo.GenerateCLICommand(filters)
}

// renderFooter renders the keybinding help footer.
func (m *SelectorModel) renderFooter() string {
	keys := []struct {
		key  string
		desc string
	}{
		{"↑↓", "navigate"},
		{"space", "toggle"},
		{"a", "all"},
		{"n", "none"},
		{"enter", "download"},
		{"c", "copy cmd"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, HelpKeyStyle.Render(k.key)+" "+HelpStyle.Render(k.desc))
	}

	return FooterStyle.Render(strings.Join(parts, " • "))
}

// Result returns the selection result (call after tea.Program ends).
func (m *SelectorModel) Result() SelectorResult {
	return m.result
}

// RunSelector runs the interactive selector TUI.
// Returns the selection result or an error.
func RunSelector(info *smartdl.RepoInfo) (*SelectorResult, error) {
	if len(info.SelectableItems) == 0 {
		return nil, fmt.Errorf("no selectable items found for %s", info.Repo)
	}

	model := NewSelectorModel(info)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run selector: %w", err)
	}

	m := finalModel.(*SelectorModel)
	result := m.Result()

	return &result, nil
}

// humanSize converts bytes to human readable format.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Ensure SelectorModel implements tea.Model.
var _ tea.Model = (*SelectorModel)(nil)
