package core

import (
	"slices"
	"strings"
	"testing"
)

func TestWrap(t *testing.T) {
	e := newEditor(t, "a\n")
	e.Resize(15, 10) // a gutter of 5, so 10 columns of text
	cases := []struct {
		s    string
		want []int
	}{
		{"", []int{0}},
		{"0123456789", []int{0}},               // exactly fits
		{"hello world again", []int{0, 6, 12}}, // after the spaces
		{"abcdefghijklmno", []int{0, 10}},      // no space: mid-word
		{"\tabcdefghi", []int{0, 7}},           // indentation isn't a break
		{"ab\tcdefghij", []int{0, 3}},          // a tab is, and counts to its stop
		{"héllo wörld", []int{0, 7}},           // runes fill a row; offsets are bytes
	}
	for _, c := range cases {
		if got := e.Wrap(c.s); !slices.Equal(got, c.want) {
			t.Errorf("Wrap(%q) = %v, want %v", c.s, got, c.want)
		}
	}
	e.Width = 0
	if got := e.Wrap(strings.Repeat("x", 100)); !slices.Equal(got, []int{0}) {
		t.Errorf("without a width: %v", got)
	}
}

func TestGutter(t *testing.T) {
	e := newEditor(t, strings.Repeat("a\n", 998)) // 999 lines with the empty last one
	if g := e.Gutter(); g != 5 {
		t.Errorf("999 lines: gutter %d, want 5", g)
	}
	e = newEditor(t, strings.Repeat("a\n", 999))
	if g := e.Gutter(); g != 6 {
		t.Errorf("1000 lines: gutter %d, want 6", g)
	}
}

func TestScrollWrapped(t *testing.T) {
	e := newEditor(t, "hello world again\nx1\nx2\nx3\n")
	e.Resize(15, 6) // four text rows; line 1 takes three
	keys(e, "j")
	if e.Top != 0 || e.TopSub != 0 {
		t.Errorf("on line 2: top %d+%d, want 0+0", e.Top, e.TopSub)
	}
	keys(e, "j") // the fifth row
	if e.Top != 0 || e.TopSub != 1 {
		t.Errorf("on line 3: top %d+%d, want 0+1", e.Top, e.TopSub)
	}
	keys(e, "g g")
	if e.Top != 0 || e.TopSub != 0 {
		t.Errorf("back on line 1: top %d+%d, want 0+0", e.Top, e.TopSub)
	}

	e.Resize(15, 4)                                        // two text rows
	e.setHead(strings.Index(e.Buf.Text.String(), "again")) // the third row
	e.scroll()
	if e.Top != 0 || e.TopSub != 1 {
		t.Errorf("at the end of line 1: top %d+%d, want 0+1", e.Top, e.TopSub)
	}
}
