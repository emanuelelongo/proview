//go:build !darwin

package gui

func registerOpenFilesHandler(func([]string)) {}

func (g *Gui) deferSessionRestoreForFinder([]string) bool { return false }
