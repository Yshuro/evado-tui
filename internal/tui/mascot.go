package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// catchy is the Evado mascot, a smiling pocket catching a todo. It is traced
// from brand/svg/evado-catcher-mark.svg in the web app and then cleaned up by
// hand, because at this size automatic downsampling turns the face to mush.
// One letter is one pixel: c cream, n coral, d dark teal, . clear.
//
// Two pixel rows share one terminal row, so keep the row count even, and keep
// every eye and the smile inside one even-odd row pair. A feature that
// straddles a pair is split across two terminal lines, and the seam between
// half-block glyphs makes each half read as a separate eye or mouth.
var catchy = []string{
	"................nnn.................",
	"............nnnnnnnn................",
	"........nnnnnnnnnnnn................",
	".....nnnnnnnnnnnnnnnn...............",
	".....nnnnnnnnnnnnnnnn...............",
	".....nnnnnnnnnnnnnnnn...............",
	".....nnnnnnnnnnnnnnnn...............",
	"......nnnnnnnnnnnnnnnnddd...........",
	"....ccnnnnnnnnnnnnnnnnddddddddcc....",
	".ccdddnnnnnnnnnnnnnnnndddddddddddc..",
	".cddddnnnnnnnnnnnnnnnnddddddddddddc.",
	".cdddddnnnnnnnnnnnnnnnndddddddddddc.",
	".cccdddnnnnnnnnnnnnnnnndddddddddccc.",
	".cccccccddddnnnnnnnnnnndddddccccccc.",
	".cccccccccccccccccccccccccccccccccc.",
	".cccccccccccccccccccccccccccccccccc.",
	".cccccccccccccccccccccccccccccccccc.",
	".cccccccccccccccccccccccccccccccccc.",
	".cccccccccccccccddccccccccccddccccc.",
	"..ccccccccccccccddccccccccccddcccc..",
	"..cccccccccccccccccccccccccccccccc..",
	"..cccccccccccccccccccccccccccccccc..",
	"..ccccccccccccccccccdcccccdccccccc..",
	"..ccccccccccccccccccdddddddccccccc..",
	"...cccccccccccccccccccccccccccccc...",
	"...cccccccccccccccccccccccccccccc...",
	"...cccccccccccccccccccccccccccccc...",
	"...cccccccccccccccccccccccccccccc...",
	"....cccccccccccccccccccccccccccc....",
	"....cccccccccccccccccccccccccccc....",
	".....cccccccccccccccccccccccccc.....",
	"......cccccccccccccccccccccccc......",
	"........cccccccccccccccccccc........",
	"...........cccccccccccccc...........",
}

// catchyRows is how many terminal rows the mascot takes.
const catchyRows = 17

var catchyColors = map[byte]lipgloss.Color{
	'c': "#FFFCF4",
	'n': "#FA5145",
	'd': "#073B46",
}

// renderCatchy draws the mascot with half-block characters: the top pixel
// is the foreground of "▀" and the bottom pixel its background. Lipgloss
// maps the hex colors down for 256 and 16 color terminals.
func renderCatchy() string {
	pixel := func(row int, x int) byte {
		if row >= len(catchy) || x >= len(catchy[row]) {
			return '.'
		}
		return catchy[row][x]
	}
	var out []string
	for row := 0; row < len(catchy); row += 2 {
		var line strings.Builder
		for x := 0; x < len(catchy[row]); x++ {
			top, bot := pixel(row, x), pixel(row+1, x)
			line.WriteString(halfBlock(top, bot))
		}
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}

func halfBlock(top, bot byte) string {
	switch {
	case top == '.' && bot == '.':
		return " "
	case bot == '.':
		return lipgloss.NewStyle().Foreground(catchyColors[top]).Render("▀")
	case top == '.':
		return lipgloss.NewStyle().Foreground(catchyColors[bot]).Render("▄")
	case top == bot:
		return lipgloss.NewStyle().Foreground(catchyColors[top]).Render("█")
	}
	return lipgloss.NewStyle().Foreground(catchyColors[top]).Background(catchyColors[bot]).Render("▀")
}
