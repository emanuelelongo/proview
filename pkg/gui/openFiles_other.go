//go:build !darwin

package gui

func registerOpenFilesHandler(func([]string)) {}
