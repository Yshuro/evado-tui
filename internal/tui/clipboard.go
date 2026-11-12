package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// clipboardTools are the programs tried, in order, to reach the system
// clipboard.
var clipboardTools = []string{"pbcopy", "wl-copy", "xclip", "xsel", "clip.exe"}

// copyToClipboard puts text on the system clipboard when a clipboard
// program is installed. Otherwise it asks the terminal to do it with an
// OSC 52 sequence, which is what makes copying work over SSH, where the
// clipboard belongs to the machine at the other end. The text is shown
// either way, for a terminal that ignores the sequence.
func (m *Model) copyToClipboard(text, what string) tea.Cmd {
	return func() tea.Msg {
		for _, bin := range clipboardTools {
			if _, err := exec.LookPath(bin); err != nil {
				continue
			}
			var c *exec.Cmd
			switch bin {
			case "xclip":
				c = exec.Command("xclip", "-selection", "clipboard")
			case "xsel":
				c = exec.Command("xsel", "--clipboard", "--input")
			default:
				c = exec.Command(bin)
			}
			c.Stdin = strings.NewReader(text)
			if err := c.Run(); err == nil {
				return toastMsg{kind: toastOK, text: what}
			}
		}
		termenv.NewOutput(os.Stdout).Copy(text)
		return copyMsg{text: text}
	}
}
