//go:build !darwin && !linux && !windows

package printing

import (
	"context"
	"fmt"
)

func printNative(context.Context, Job, string) error {
	return fmt.Errorf("printing is unavailable on this platform")
}
