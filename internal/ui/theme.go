package ui

import (
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"golang.org/x/term"
)

// hiss's colours are picked for dark terminals. Most are the terminal's own
// ANSI colours, which a light theme already adapts; the ones below are not,
// and are hard to read on a light background, so they have light variants.

// UseLightTheme switches to colours for a light terminal background. Call
// it before drawing.
func UseLightTheme() {
	styleSel = tcell.StyleDefault.Background(tcell.NewRGBColor(0xc6, 0xdb, 0xf7))
	styleMatch = tcell.StyleDefault.Background(tcell.NewRGBColor(0xff, 0xe0, 0x8a)).Foreground(color.Black)
	styleRelated = tcell.StyleDefault.Background(tcell.NewRGBColor(0xdd, 0xdd, 0xdd))
	styleMenu = tcell.StyleDefault.Background(tcell.NewRGBColor(0xe4, 0xe4, 0xe4)).Foreground(color.Black)
	styleMenuKey = styleMenu.Bold(true).Foreground(tcell.NewRGBColor(0x1d, 0x4e, 0xd8))
	styleYourNote = tcell.StyleDefault.Foreground(tcell.NewRGBColor(0x9a, 0x67, 0x00))
	styleWarning = styleYourNote // terminal yellow is unreadable on white
}

// LightBackground reports whether the terminal has a light background.
// HISS_THEME=light or dark decides if set; otherwise the terminal is asked
// for its background colour. Terminals that don't say count as dark.
// It must run before the screen takes over the terminal.
func LightBackground() bool {
	switch os.Getenv("HISS_THEME") {
	case "light":
		return true
	case "dark":
		return false
	}
	light, _ := isLight(queryBackground(time.Second))
	return light
}

var (
	osc11 = regexp.MustCompile(`\x1b\]11;rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`)
	da1   = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// queryBackground asks the terminal for its background colour (OSC 11) and
// returns what it sent back. It follows with a device attributes query,
// which every terminal answers, so reading up to that reply never waits on
// a terminal that ignores the first.
func queryBackground(timeout time.Duration) []byte {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer term.Restore(fd, state)
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b[c"); err != nil {
		return nil
	}
	reply := make(chan []byte, 1)
	go func() {
		var got []byte
		buf := make([]byte, 256)
		for !da1.Match(got) {
			n, err := tty.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				break
			}
		}
		reply <- got
	}()
	select {
	case got := <-reply:
		return got
	case <-time.After(timeout):
		return nil
	}
}

// isLight reads an OSC 11 reply and reports whether the colour is light.
func isLight(reply []byte) (light, ok bool) {
	m := osc11.FindSubmatch(reply)
	if m == nil {
		return false, false
	}
	var c [3]float64
	for i, hex := range m[1:] {
		v, _ := strconv.ParseUint(string(hex), 16, 16)
		c[i] = float64(v) / float64(uint64(1)<<(4*len(hex))-1) // 1 to 4 hex digits per channel
	}
	return 0.2126*c[0]+0.7152*c[1]+0.0722*c[2] > 0.5, true
}
