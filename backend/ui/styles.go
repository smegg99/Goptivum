// ui/styles.go

// Package ui is everything this repository's programs print. One palette and
// one set of styles, so the server and its tools look like one product and not
// a pile of format strings.
package ui

import "charm.land/lipgloss/v2"

// Styles is the whole palette. Nothing outside this file builds a color.
type Styles struct {
	Title  lipgloss.Style
	Key    lipgloss.Style
	Ink    lipgloss.Style
	Muted  lipgloss.Style
	Accent lipgloss.Style
	Good   lipgloss.Style
	Warn   lipgloss.Style
	Bad    lipgloss.Style
	Panel  lipgloss.Style
	Header lipgloss.Style
	Cell   lipgloss.Style
	Border lipgloss.Style
}

// NewStyles resolves the palette for a light or a dark terminal, so one
// definition covers both.
func NewStyles(isDark bool) Styles {
	pick := lipgloss.LightDark(isDark)

	// The accent Goptivum Desktop uses, so the server and the app a school
	// sees are recognisably one product.
	accent := lipgloss.Color("#F0B429")
	ink := pick(lipgloss.Color("#1c1c1c"), lipgloss.Color("#e6e6e6"))
	muted := pick(lipgloss.Color("#6b6b6b"), lipgloss.Color("#8a8a8a"))
	line := pick(lipgloss.Color("#d8d8d8"), lipgloss.Color("#3a3a3a"))

	// ANSI indices, so state keeps its meaning in a sixteen-color terminal.
	good := lipgloss.Color("2")
	warn := lipgloss.Color("3")
	bad := lipgloss.Color("1")

	return Styles{
		Title:  lipgloss.NewStyle().Foreground(ink).Bold(true),
		Key:    lipgloss.NewStyle().Foreground(muted).Width(keyWidth),
		Ink:    lipgloss.NewStyle().Foreground(ink),
		Muted:  lipgloss.NewStyle().Foreground(muted),
		Accent: lipgloss.NewStyle().Foreground(accent).Bold(true),
		Good:   lipgloss.NewStyle().Foreground(good),
		Warn:   lipgloss.NewStyle().Foreground(warn),
		Bad:    lipgloss.NewStyle().Foreground(bad),
		Panel:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		Header: lipgloss.NewStyle().Foreground(muted).Bold(true).Padding(0, 1),
		Cell:   lipgloss.NewStyle().Foreground(ink).Padding(0, 1),
		Border: lipgloss.NewStyle().Foreground(line),
	}
}

// keyWidth aligns the label column of every field block this program prints.
const keyWidth = 16
