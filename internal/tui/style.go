package tui

import (
	"hash/fnv"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// palette is one colour scheme. Every colour on screen comes from here, so
// a theme is a complete look rather than a tint over a fixed one.
type scheme struct {
	name  string
	label string

	fg, muted, faint string
	// accent is the brand colour; accent2 is where a gradient from it ends.
	accent, accent2         string
	green, red, amber, blue string
	// panel backs the selected row, and border frames the panes.
	panel, border string
}

// schemes is every built-in theme, in the order the picker shows them.
var schemes = []scheme{
	{
		name: "evado", label: "Evado",
		fg: "#F6F1E9", muted: "#ADA59D", faint: "#756D67",
		accent: "#FF6A55", accent2: "#FFC15E",
		green: "#7BD88F", red: "#FF4F81", amber: "#F2C14E", blue: "#5CC8D7",
		panel: "#2E2826", border: "#4A4340",
	},
	{
		name: "evado-light", label: "Evado Light",
		fg: "#1F1B18", muted: "#6E665F", faint: "#A69E96",
		accent: "#E23D2E", accent2: "#E58A00",
		green: "#1C7A4A", red: "#C2185B", amber: "#8A5A00", blue: "#0B6B7A",
		panel: "#F1E9DC", border: "#D8CFC2",
	},
	{
		name: "catppuccin", label: "Catppuccin Mocha",
		fg: "#CDD6F4", muted: "#A6ADC8", faint: "#6C7086",
		accent: "#CBA6F7", accent2: "#F5C2E7",
		green: "#A6E3A1", red: "#F38BA8", amber: "#F9E2AF", blue: "#89B4FA",
		panel: "#313244", border: "#45475A",
	},
	{
		name: "tokyonight", label: "Tokyo Night",
		fg: "#C0CAF5", muted: "#9AA5CE", faint: "#565F89",
		accent: "#7AA2F7", accent2: "#BB9AF7",
		green: "#9ECE6A", red: "#F7768E", amber: "#E0AF68", blue: "#7DCFFF",
		panel: "#292E42", border: "#3B4261",
	},
	{
		name: "gruvbox", label: "Gruvbox",
		fg: "#EBDBB2", muted: "#BDAE93", faint: "#7C6F64",
		accent: "#FE8019", accent2: "#FABD2F",
		green: "#B8BB26", red: "#FB4934", amber: "#FABD2F", blue: "#83A598",
		panel: "#3C3836", border: "#504945",
	},
	{
		name: "nord", label: "Nord",
		fg: "#ECEFF4", muted: "#A7B1C2", faint: "#616E88",
		accent: "#88C0D0", accent2: "#B48EAD",
		green: "#A3BE8C", red: "#BF616A", amber: "#EBCB8B", blue: "#81A1C1",
		panel: "#3B4252", border: "#4C566A",
	},
	{
		name: "dracula", label: "Dracula",
		fg: "#F8F8F2", muted: "#BFBFD0", faint: "#6272A4",
		accent: "#BD93F9", accent2: "#FF79C6",
		green: "#50FA7B", red: "#FF5555", amber: "#F1FA8C", blue: "#8BE9FD",
		panel: "#44475A", border: "#565A73",
	},
	{
		name: "rosepine", label: "Rosé Pine",
		fg: "#E0DEF4", muted: "#908CAA", faint: "#6E6A86",
		accent: "#EBBCBA", accent2: "#C4A7E7",
		green: "#95D5B2", red: "#EB6F92", amber: "#F6C177", blue: "#9CCFD8",
		panel: "#26233A", border: "#403D52",
	},
}

// current is the scheme on screen.
var current scheme

var (
	colFg, colMuted, colFaint, colAccent, colAccent2 lipgloss.Color
	colGreen, colRed, colAmber, colBlue              lipgloss.Color
	colPanel, colBorder                              lipgloss.Color
	// tagColors tint the list names, so a list is recognisable by colour
	// before its name is read.
	tagColors []lipgloss.Color
)

var (
	styTitle, styMuted, styFaint, styBold, styKey lipgloss.Style
	styOK, styWarn, styErr, stySel, styOver       lipgloss.Style
	stySoon, styIdle, styBox, styAccent           lipgloss.Style
	// styPill is a filled label: the active tab, a button, the mode.
	styPill lipgloss.Style
)

// NO_COLOR, or a terminal that cannot do colour at all, drops to plain text.
func init() {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	applyTheme("")
}

// schemeByName finds a theme, reporting whether the name is one.
func schemeByName(name string) (scheme, bool) {
	for _, s := range schemes {
		if s.name == name {
			return s, true
		}
	}
	return scheme{}, false
}

// applyTheme switches the colours. "light" and "dark" pick the house theme
// for that background, a theme name picks that theme, and anything else
// follows the terminal's own background.
func applyTheme(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "dark":
		name = "evado"
	case "light":
		name = "evado-light"
	}
	s, ok := schemeByName(name)
	if !ok {
		s = schemes[0]
		if !lipgloss.HasDarkBackground() {
			s = schemes[1]
		}
	}
	current = s
	colFg, colMuted, colFaint = lipgloss.Color(s.fg), lipgloss.Color(s.muted), lipgloss.Color(s.faint)
	colAccent, colAccent2 = lipgloss.Color(s.accent), lipgloss.Color(s.accent2)
	colGreen, colRed = lipgloss.Color(s.green), lipgloss.Color(s.red)
	colAmber, colBlue = lipgloss.Color(s.amber), lipgloss.Color(s.blue)
	colPanel, colBorder = lipgloss.Color(s.panel), lipgloss.Color(s.border)
	tagColors = []lipgloss.Color{colBlue, colAccent2, colGreen, colAmber, colAccent}

	styTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styMuted = lipgloss.NewStyle().Foreground(colMuted)
	styFaint = lipgloss.NewStyle().Foreground(colFaint)
	styBold = lipgloss.NewStyle().Foreground(colFg).Bold(true)
	styKey = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styOK = lipgloss.NewStyle().Foreground(colGreen)
	styWarn = lipgloss.NewStyle().Foreground(colAmber)
	styErr = lipgloss.NewStyle().Foreground(colRed)
	stySel = lipgloss.NewStyle().Foreground(colFg).Background(colPanel).Bold(true)
	styOver = lipgloss.NewStyle().Foreground(colRed)
	stySoon = lipgloss.NewStyle().Foreground(colBlue)
	styIdle = lipgloss.NewStyle().Foreground(colMuted)
	styBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(0, 1)
	styAccent = lipgloss.NewStyle().Foreground(colAccent)
	styPill = lipgloss.NewStyle().Foreground(lipgloss.Color(pillText(s.accent))).Background(colAccent).Bold(true)
}

// pillText picks black or white text for a filled label, whichever reads
// better on the colour behind it.
func pillText(bg string) string {
	c, err := colorful.Hex(bg)
	if err != nil {
		return "#000000"
	}
	if l, _, _ := c.Lab(); l > 0.6 {
		return "#1A1614"
	}
	return "#FFFFFF"
}

// plain reports whether colour is off, in which case the decorative
// helpers below return their text untouched.
func plain() bool { return lipgloss.ColorProfile() == termenv.Ascii }

// gradient colours text from one colour to another across its width.
func gradient(text, from, to string, bold bool) string {
	if plain() {
		return text
	}
	a, errA := colorful.Hex(from)
	b, errB := colorful.Hex(to)
	if errA != nil || errB != nil {
		return text
	}
	runes := []rune(text)
	var out strings.Builder
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(a.BlendHcl(b, t).Clamped().Hex())).Bold(bold)
		out.WriteString(st.Render(string(r)))
	}
	return out.String()
}

// brandGradient is gradient in the theme's own two colours.
func brandGradient(text string) string {
	return gradient(text, current.accent, current.accent2, true)
}

// tagColor is a stable colour for an id, so a list keeps its colour from
// one run to the next.
func tagColor(id string) lipgloss.Color {
	if id == "" || len(tagColors) == 0 {
		return colMuted
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return tagColors[int(h.Sum32())%len(tagColors)]
}

// meter is a small progress bar, filled in the theme gradient.
func meter(done, total, w int) string {
	if w <= 0 {
		return ""
	}
	fill := 0
	if total > 0 {
		fill = min(done*w/total, w)
		if done > 0 && fill == 0 {
			fill = 1
		}
	}
	return gradient(strings.Repeat("━", fill), current.accent, current.accent2, false) +
		styFaint.Render(strings.Repeat("━", w-fill))
}

// sparkBars are the eight heights a sparkline can draw.
var sparkBars = []rune("▁▂▃▄▅▆▇█")

// sparkline draws one bar per value, scaled to the tallest.
func sparkline(vals []int) string {
	top := 0
	for _, v := range vals {
		top = max(top, v)
	}
	var b strings.Builder
	for _, v := range vals {
		if v == 0 || top == 0 {
			b.WriteString(styFaint.Render("▁"))
			continue
		}
		b.WriteString(styAccent.Render(string(sparkBars[min(v*(len(sparkBars)-1)/top, len(sparkBars)-1)])))
	}
	return b.String()
}

// frameOpts is what a pane's frame carries besides its body.
type frameOpts struct {
	title string
	// foot is set into the bottom edge, on the right.
	foot    string
	focused bool
}

// frame draws body inside a rounded box exactly w by h cells, with the
// title set into the top edge. The body is clipped and padded to fit, so
// panes line up whatever they hold.
func frame(body string, w, h int, o frameOpts) string {
	if w < 6 || h < 3 {
		return clipBlock(body, max(w, 0), max(h, 0))
	}
	edge := lipgloss.NewStyle().Foreground(colBorder)
	titleSty := styMuted
	if o.focused {
		edge = lipgloss.NewStyle().Foreground(colAccent)
		titleSty = styTitle
	}
	inner := w - 2

	top := edge.Render("╭─")
	used := 2
	if o.title != "" {
		t := truncate(o.title, max(inner-4, 1))
		// A title that already carries its own colours is left as it is.
		if !strings.Contains(t, "\x1b") {
			t = titleSty.Render(t)
		}
		top += " " + t + " "
		used += visibleWidth(t) + 2
	}
	top += edge.Render(strings.Repeat("─", max(w-used-1, 0)) + "╮")

	bottom := edge.Render("╰")
	used = 1
	if o.foot != "" && visibleWidth(o.foot)+6 <= w {
		fill := w - visibleWidth(o.foot) - 5
		bottom += edge.Render(strings.Repeat("─", fill)) + " " + o.foot + " " + edge.Render("─╯")
	} else {
		bottom += edge.Render(strings.Repeat("─", max(w-used-1, 0)) + "╯")
	}

	lines := strings.Split(clipBlock(body, inner, h-2), "\n")
	side := edge.Render("│")
	for i, l := range lines {
		lines[i] = side + l + side
	}
	return top + "\n" + strings.Join(lines, "\n") + "\n" + bottom
}

// clipBlock makes a block exactly w by h cells.
func clipBlock(s string, w, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = padTo(l, w)
	}
	return strings.Join(lines, "\n")
}

// joinPanes sets blocks side by side. Each is already a fixed size, so
// this is plain concatenation line by line.
func joinPanes(panes ...string) string {
	var rows []string
	for _, p := range panes {
		if p == "" {
			continue
		}
		for i, l := range strings.Split(p, "\n") {
			if i >= len(rows) {
				rows = append(rows, "")
			}
			rows[i] += l
		}
	}
	return strings.Join(rows, "\n")
}

// spinnerFrames is the busy indicator, one frame per animation tick.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// label is a small heading inside a pane.
func label(s string) string {
	return lipgloss.NewStyle().Foreground(colFaint).Bold(true).Render(strings.ToUpper(s))
}
