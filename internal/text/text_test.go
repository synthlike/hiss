package text

import (
	"bytes"
	"fmt"
	"testing"
)

var (
	disk   = Origin{}
	you    = Origin{Author: "you"}
	claude = Origin{Author: "claude", Session: "s1", Turn: 3, Proposal: 7}
)

func TestEditsAndSpans(t *testing.T) {
	a := New([]byte("hello world\n"), disk)
	b := a.Replace(6, 11, []byte("there"), claude) // hello there\n
	c := b.Insert(0, []byte("oh, "), you)          // oh, hello there\n

	if got := a.String(); got != "hello world\n" {
		t.Errorf("old snapshot changed: %q", got)
	}
	if got := c.String(); got != "oh, hello there\n" {
		t.Fatalf("got %q", got)
	}
	want := []Span{{0, 4, you}, {4, 10, disk}, {10, 15, claude}, {15, 16, disk}}
	if got := c.Spans(0, c.Len()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("spans\n got %v\nwant %v", got, want)
	}
}

func TestTypingCoalesces(t *testing.T) {
	x := New([]byte("ab"), disk)
	for i, c := range []byte("xyz") {
		x = x.Insert(1+i, []byte{c}, you)
	}
	if got := x.String(); got != "axyzb" {
		t.Fatalf("got %q", got)
	}
	if n := len(x.pieces); n != 3 {
		t.Errorf("want 3 pieces after typing a run, got %d", n)
	}
}

func TestLines(t *testing.T) {
	x := New([]byte("one\ntwo\n"), disk).Insert(4, []byte("1.5\n"), you)
	// one\n1.5\ntwo\n
	if n := x.LineCount(); n != 4 {
		t.Errorf("LineCount = %d, want 4", n)
	}
	for line, off := range []int{0, 4, 8, 12} {
		if got := x.LineStart(line); got != off {
			t.Errorf("LineStart(%d) = %d, want %d", line, got, off)
		}
		if got := x.LineOf(off); got != line {
			t.Errorf("LineOf(%d) = %d, want %d", off, got, line)
		}
	}
	if got := x.LineOf(3); got != 0 {
		t.Errorf("LineOf(newline) = %d, want 0", got)
	}
}

func TestZeroValue(t *testing.T) {
	var x Text
	x = x.Insert(0, []byte("hi"), you)
	if x.String() != "hi" || x.LineCount() != 1 {
		t.Errorf("got %q lines=%d", x.String(), x.LineCount())
	}
}

func TestEditMap(t *testing.T) {
	e := Edit{Start: 4, End: 6, Len: 5} // replace 2 bytes with 5
	cases := []struct {
		off   int
		after bool
		want  int
	}{
		{3, false, 3}, {4, false, 4}, {4, true, 9}, {5, false, 4},
		{6, true, 9}, {6, false, 4}, {7, false, 10},
	}
	for _, c := range cases {
		if got := e.Map(c.off, c.after); got != c.want {
			t.Errorf("Map(%d, %v) = %d, want %d", c.off, c.after, got, c.want)
		}
	}
	ins := Edit{Start: 2, End: 2, Len: 3}
	if ins.Map(2, false) != 2 || ins.Map(2, true) != 5 {
		t.Error("insertion bias wrong")
	}
}

// model is a byte-per-byte reference implementation.
type model struct {
	b []byte
	o []Origin
}

func (m model) replace(start, end int, s []byte, o Origin) model {
	var n model
	n.b = append(append(append([]byte{}, m.b[:start]...), s...), m.b[end:]...)
	n.o = append([]Origin{}, m.o[:start]...)
	for range s {
		n.o = append(n.o, o)
	}
	n.o = append(n.o, m.o[end:]...)
	return n
}

func FuzzReplace(f *testing.F) {
	f.Add([]byte("hello\nworld\n"), []byte{0, 3, 1, 'x', 5, 0, 2, 9, 9, 1, 'y', 1, 4, 4, 2})
	f.Add([]byte(""), []byte{2, 0, 0, 0, '\n'})
	f.Fuzz(func(t *testing.T, init, ops []byte) {
		origins := []Origin{disk, you, claude}
		type version struct {
			t Text
			m model
		}
		m0 := model{b: append([]byte{}, init...), o: make([]Origin, len(init))}
		vs := []version{{New(append([]byte{}, init...), disk), m0}}
		for len(ops) >= 5 {
			v := vs[int(ops[0])%len(vs)] // edit any version, not just the latest
			n := v.m.b
			start := int(ops[1]) % (len(n) + 1)
			end := start + int(ops[2])%(len(n)-start+1)
			s := bytes.Repeat([]byte{ops[3]}, int(ops[4])%4)
			o := origins[int(ops[4])%len(origins)]
			ops = ops[5:]
			vs = append(vs, version{v.t.Replace(start, end, s, o), v.m.replace(start, end, s, o)})
		}
		for i, v := range vs {
			checkAgainst(t, i, v.t, v.m)
		}
	})
}

func checkAgainst(t *testing.T, i int, x Text, m model) {
	t.Helper()
	if !bytes.Equal(x.Bytes(), m.b) {
		t.Fatalf("v%d: text %q, want %q", i, x.Bytes(), m.b)
	}
	for _, s := range x.Spans(0, x.Len()) {
		for j := s.Start; j < s.End; j++ {
			if m.o[j] != s.Origin {
				t.Fatalf("v%d: origin at %d = %v, want %v", i, j, s.Origin, m.o[j])
			}
		}
	}
	nls := bytes.Count(m.b, nl)
	if x.LineCount() != nls+1 {
		t.Fatalf("v%d: LineCount %d, want %d", i, x.LineCount(), nls+1)
	}
	line := 0
	for off := 0; off <= len(m.b); off++ {
		if x.LineOf(off) != line {
			t.Fatalf("v%d: LineOf(%d) = %d, want %d", i, off, x.LineOf(off), line)
		}
		if off == 0 || m.b[off-1] == '\n' {
			if x.LineStart(line) != off {
				t.Fatalf("v%d: LineStart(%d) = %d, want %d", i, line, x.LineStart(line), off)
			}
		}
		if off < len(m.b) && m.b[off] == '\n' {
			line++
		}
	}
}
