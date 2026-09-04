//go:build windows

package holder

import (
	"github.com/airtonix/hrdx/internal/winproc"
	"github.com/aymanbagabas/go-pty"
)

func foregroundName(_ pty.Pty, rootPID int) string {
	return winproc.ForegroundName(rootPID)
}
