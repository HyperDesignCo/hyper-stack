//go:build !windows

package app

import (
	"context"
	"errors"
)

var errMoveUnsupported = errors.New("moving the install is only available on Windows in this version")

func copyTree(context.Context, string, string, []string) error { return errMoveUnsupported }

func freeBytes(string) (uint64, error) { return 0, errMoveUnsupported }
