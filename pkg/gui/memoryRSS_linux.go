//go:build linux

package gui

import (
	"fmt"
	"os"
)

func processResidentBytes() (uint64, error) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	var total, resident uint64
	if _, err := fmt.Sscan(string(data), &total, &resident); err != nil {
		return 0, err
	}
	return resident * uint64(os.Getpagesize()), nil
}
