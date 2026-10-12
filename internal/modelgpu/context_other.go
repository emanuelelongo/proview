//go:build !darwin || !cgo

package modelgpu

import "fmt"

func newDevice() (device, error) { return nil, fmt.Errorf("GPU prototype requires macOS with CGO") }
