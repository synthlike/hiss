package syntax

import "bytes"

// Lisp covers Common Lisp, Scheme, Racket, Clojure, Emacs Lisp, Fennel and
// Janet. Symbols like define-syntax and set! take punctuation, and ' quotes
// rather than opens a string, so it has its own lexer.
var Lisp = &Lang{Name: "lisp", LineComment: ";", lex: lexLisp}

var lispWords = words(`define define-syntax define-record-type define-values lambda let let* letrec
	let-values if when unless cond case and or not do begin quote quasiquote set! else #t #f #true #false
	defun defmacro defvar defparameter defconstant defconst defcustom defgeneric defmethod defstruct
	defclass defpackage in-package flet labels progn prog1 loop setq setf unwind-protect handler-case
	defn defn- def defonce defprotocol defrecord deftype defmulti fn recur ns try catch finally throw
	require import provide module use-package local var each for while match nil t true false`)

// lexLisp marks ; and #| |# comments, strings, characters (#\a, \a),
// numbers, :keywords (as numbers, being data) and special forms.
func lexLisp(src []byte) []Span {
	var out []Span
	for i := 0; i < len(src); {
		c := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		j := i + 1
		switch {
		case c == ';':
			j = lineEnd(src, i)
			out = append(out, Span{i, j, Comment})
		case c == '#' && next == '|':
			j = len(src)
			if k := bytes.Index(src[i+2:], []byte("|#")); k >= 0 {
				j = i + 2 + k + 2
			}
			out = append(out, Span{i, j, Comment})
		case c == '"': // strings may span lines
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(src))
			out = append(out, Span{i, j, String})
		case c == '\\' || c == '#' && next == '\\':
			if c == '#' {
				j++
			}
			if j < len(src) { // the character itself, even ( or ;
				j++
			}
			for j < len(src) && !lispDelim(src[j]) { // #\space
				j++
			}
			out = append(out, Span{i, j, String})
		case lispDelim(c):
		default:
			for j < len(src) && !lispDelim(src[j]) {
				j++
			}
			switch w := string(src[i:j]); {
			case lispNumber(w) || len(w) > 1 && w[0] == ':':
				out = append(out, Span{i, j, Number})
			case lispWords[w]:
				out = append(out, Span{i, j, Keyword})
			}
		}
		i = j
	}
	return out
}

func lispDelim(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '(', ')', '[', ']', '{', '}', '"', ';', '\'', '`', ',':
		return true
	}
	return false
}

// lispNumber reports tokens like 42, -3.5e2, 1/2 and .5.
func lispNumber(w string) bool {
	if len(w) > 1 && (w[0] == '+' || w[0] == '-') {
		w = w[1:]
	}
	if len(w) > 1 && w[0] == '.' {
		w = w[1:]
	}
	return w != "" && isDigit(w[0])
}
