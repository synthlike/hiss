package ui

import (
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/synthlike/hiss/internal/syntax"
	"github.com/synthlike/hiss/internal/text"
)

// maxLexPrefix bounds how much text before the screen is lexed to get
// multi-line strings and comments right; beyond it lexing starts at the top
// line and may be wrong there.
const maxLexPrefix = 512 << 10

var syntaxStyles = map[syntax.Kind]tcell.Style{
	syntax.Comment:  tcell.StyleDefault.Foreground(color.Gray),
	syntax.String:   tcell.StyleDefault.Foreground(color.Olive),
	syntax.Number:   tcell.StyleDefault.Foreground(color.Purple),
	syntax.Keyword:  tcell.StyleDefault.Foreground(color.Blue),
	syntax.Heading:  tcell.StyleDefault.Foreground(color.Blue).Bold(true),
	syntax.Strong:   tcell.StyleDefault.Bold(true),
	syntax.Emphasis: tcell.StyleDefault.Italic(true),
}

// highlight lexes t from the top of the file (or of the screen, for very
// large files) to the end of the last visible line. It returns the style of a
// byte offset; offsets must be asked for in ascending order.
func highlight(path string, t text.Text, top, rows int) func(off int) tcell.Style {
	plain := func(int) tcell.Style { return styleText }
	lang := syntax.ForPath(path)
	if lang == nil || t.Len() == 0 {
		return plain
	}
	begin := 0
	if s := t.LineStart(min(top, t.LineCount()-1)); s > maxLexPrefix {
		begin = s
	}
	end := t.Len()
	if last := top + rows; last+1 < t.LineCount() {
		end = t.LineStart(last + 1)
	}
	spans := lang.Lex(t.Slice(begin, end))
	i := 0
	return func(off int) tcell.Style {
		off -= begin
		for i < len(spans) && spans[i].End <= off {
			i++
		}
		if i < len(spans) && spans[i].Start <= off {
			return syntaxStyles[spans[i].Kind]
		}
		return styleText
	}
}
