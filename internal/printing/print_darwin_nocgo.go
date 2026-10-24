//go:build darwin && !cgo

package printing

import (
	"context"
	"fmt"
)

func printNative(context.Context, Job, string) error {
	return fmt.Errorf("native printing requires a CGO build")
}
