package core

import "slices"

// Jumps: moves that can go far (search, gg/ge, :<line>, hunks, trail steps,
// pickers, :e) record where the cursor was. C-o goes back through those
// places and C-i (or tab, which terminals often send for it) forward again.
// Positions are anchors, so they follow edits.

type jumpPos struct {
	buf *Buffer
	at  *anchor
}

type jumpList struct {
	list []jumpPos
	i    int // the entry C-o and C-i are on; len(list) when not walking the list
}

const maxJumps = 100

// pushJump records the cursor position before a jump, dropping any newer
// positions left over from walking back.
func (e *Editor) pushJump() {
	j := &e.jumps
	for _, p := range j.list[j.i:] {
		p.buf.release(p.at)
	}
	j.list = j.list[:j.i]
	if n := len(j.list); n == 0 || j.list[n-1].buf != e.Buf || e.Buf.Text.LineOf(j.list[n-1].at.off) != e.line() {
		j.list = append(j.list, jumpPos{e.Buf, e.Buf.anchor(e.Sel.Head)})
	}
	if len(j.list) > maxJumps {
		j.list[0].buf.release(j.list[0].at)
		j.list = j.list[1:]
	}
	j.i = len(j.list)
}

// jumpBack goes to the position before the current one in the jump list.
func (e *Editor) jumpBack() {
	j := &e.jumps
	if j.i == len(j.list) { // starting to walk back: remember here, for C-i
		e.pushJump()
		j.i = len(j.list) - 1
	}
	for j.i > 0 {
		j.i--
		if e.goJump(j.list[j.i]) {
			return
		}
	}
	e.Msg = "no older position"
}

// jumpForward undoes a jumpBack.
func (e *Editor) jumpForward() {
	j := &e.jumps
	for j.i < len(j.list)-1 {
		j.i++
		if e.goJump(j.list[j.i]) {
			return
		}
	}
	e.Msg = "no newer position"
}

// goJump moves to p, reporting false if its buffer has been closed.
func (e *Editor) goJump(p jumpPos) bool {
	if !slices.Contains(e.Bufs, p.buf) {
		return false
	}
	e.SwitchTo(p.buf)
	e.setHead(min(p.at.off, p.buf.Text.Len()))
	e.revealLine(e.line())
	return true
}
