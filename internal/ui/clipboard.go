package ui

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
)

// copyToClipboard puts text on the system clipboard. It always emits OSC 52
// (which modern terminals map to the clipboard, including over ssh) and
// additionally invokes a native clipboard tool when one exists.
func copyToClipboard(text string) {
	if text == "" {
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, _ = os.Stdout.WriteString("\x1b]52;c;" + encoded + "\x07")

	candidates := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		command := exec.Command(path, candidate[1:]...)
		command.Stdin = strings.NewReader(text)
		_ = command.Run()
		return
	}
}
