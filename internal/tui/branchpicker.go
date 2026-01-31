// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/smartdl"
)

// BranchPickerResult contains the result of branch selection.
type BranchPickerResult struct {
	// Selected is the chosen branch/tag name.
	Selected string

	// Cancelled indicates if the user cancelled selection.
	Cancelled bool
}

// refItem represents a branch or tag in the picker.
type refItem struct {
	Ref       smartdl.RepoRef
	Index     int
	IsDefault bool
}

// BranchPickerModel is the bubbletea model for branch/tag selection.
type BranchPickerModel struct {
	repo     string
	branches []refItem
	tags     []refItem
	allItems []refItem

	cursor int
	result BranchPickerResult
	done   bool
}

// NewBranchPickerModel creates a new branch picker from repo refs.
func NewBranchPickerModel(repo string, refs []smartdl.RepoRef) *BranchPickerModel {
	m := &BranchPickerModel{
		repo: repo,
	}

	// Separate branches and tags
	idx := 0
	for _, ref := range refs {
		item := refItem{
			Ref:       ref,
			Index:     idx,
			IsDefault: ref.Name == "main" || ref.Name == "master",
		}

		if ref.Type == "branch" {
			m.branches = append(m.branches, item)
		} else {
			m.tags = append(m.tags, item)
		}
		m.allItems = append(m.allItems, item)
		idx++
	}

	// Default cursor to "main" if it exists
	for i, item := range m.allItems {
		if item.Ref.Name == "main" {
			m.cursor = i
			break
		}
	}

	return m
}

// Init implements tea.Model.
func (m *BranchPickerModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *BranchPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.result.Cancelled = true
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.allItems)-1 {
				m.cursor++
			}

		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.allItems) {
				m.result.Selected = m.allItems[m.cursor].Ref.Name
				m.done = true
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m *BranchPickerModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder

	// Header
	title := TitleStyle.Render("Select Branch/Tag")
	subtitle := SubtitleStyle.Render(m.repo)
	b.WriteString(title + "\n")
	b.WriteString(subtitle + "\n\n")

	b.WriteString(SubtitleStyle.Render("This repository has multiple versions. Select which one to analyze:") + "\n\n")

	// Branches section
	if len(m.branches) > 0 {
		b.WriteString(CategoryStyle.Render("Branches") + "\n\n")
		for _, item := range m.branches {
			b.WriteString(m.renderRefItem(item))
		}
		b.WriteString("\n")
	}

	// Tags section
	if len(m.tags) > 0 {
		b.WriteString(CategoryStyle.Render("Tags") + "\n\n")
		for _, item := range m.tags {
			b.WriteString(m.renderRefItem(item))
		}
		b.WriteString("\n")
	}

	// Footer
	footer := m.renderFooter()
	b.WriteString(footer)

	return b.String()
}

// renderRefItem renders a single ref item.
func (m *BranchPickerModel) renderRefItem(item refItem) string {
	// Cursor indicator
	cursor := "  "
	if m.cursor == item.Index {
		cursor = CursorStyle.Render("> ")
	}

	// Icon based on type
	icon := "  "
	if item.Ref.Type == "branch" {
		icon = SubtitleStyle.Render("") + " "
	} else {
		icon = SubtitleStyle.Render("") + " "
	}

	// Name with default indicator
	name := item.Ref.Name
	if item.IsDefault {
		name = name + " " + SuccessStyle.Render("(default)")
	}

	// Highlight current selection
	var line string
	if m.cursor == item.Index {
		line = fmt.Sprintf("%s%s%s\n", cursor, icon, SelectedItemStyle.Render(name))
	} else {
		line = fmt.Sprintf("%s%s%s\n", cursor, icon, ItemStyle.Render(name))
	}

	return line
}

// renderFooter renders the keybinding help footer.
func (m *BranchPickerModel) renderFooter() string {
	keys := []struct {
		key  string
		desc string
	}{
		{"↑↓", "navigate"},
		{"enter", "select"},
		{"q", "cancel"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, HelpKeyStyle.Render(k.key)+" "+HelpStyle.Render(k.desc))
	}

	return FooterStyle.Render(strings.Join(parts, " • "))
}

// Result returns the selection result.
func (m *BranchPickerModel) Result() BranchPickerResult {
	return m.result
}

// RunBranchPicker runs the branch picker TUI.
// Returns the selected branch name or empty string if cancelled.
func RunBranchPicker(repo string, refs []smartdl.RepoRef) (*BranchPickerResult, error) {
	if len(refs) == 0 {
		return &BranchPickerResult{Selected: "main"}, nil
	}

	// If only one ref, return it directly
	if len(refs) == 1 {
		return &BranchPickerResult{Selected: refs[0].Name}, nil
	}

	model := NewBranchPickerModel(repo, refs)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run branch picker: %w", err)
	}

	m := finalModel.(*BranchPickerModel)
	result := m.Result()

	return &result, nil
}

// Ensure BranchPickerModel implements tea.Model.
var _ tea.Model = (*BranchPickerModel)(nil)
