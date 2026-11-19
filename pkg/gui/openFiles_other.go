//go:build !darwin

package gui

func registerOpenFilesHandler(func([]string)) {}

func (g *Gui) deferFinderLaunch([]string) bool { return false }
