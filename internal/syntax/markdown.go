package syntax

import "bytes"

// Markdown is lexed line by line rather than with the generic rules.
var Markdown = &Lang{Name: "markdown", lex: lexMarkdown}

// lexMarkdown marks headings, fenced code, quotes, rules, list markers and
// HTML comments by line, then inline code, emphasis and links within a line.
func lexMarkdown(src []byte) []Span {
	var out []Span
	var fence []byte // the open code fence, if inside one
	for i := 0; i < len(src); {
		end := lineEnd(src, i)
		line := bytes.TrimLeft(src[i:end], " ")
		ind := end - len(line)
		switch {
		case fence != nil:
			out = append(out, Span{i, end, String})
			if bytes.HasPrefix(line, fence) {
				fence = nil
			}
		case bytes.HasPrefix(line, []byte("```")) || bytes.HasPrefix(line, []byte("~~~")):
			fence = line[:3]
			out = append(out, Span{i, end, String})
		case bytes.HasPrefix(line, []byte("<!--")):
			close := len(src)
			if j := bytes.Index(src[ind:], []byte("-->")); j >= 0 {
				close = ind + j + 3
			}
			out = append(out, Span{ind, close, Comment})
			end = lineEnd(src, close)
		case isHeading(line) || onlyOf(line, "="):
			out = append(out, Span{ind, end, Heading})
		case len(line) > 0 && line[0] == '>':
			out = append(out, Span{ind, end, Comment})
		case onlyOf(line, "-") || onlyOf(line, "*") || onlyOf(line, "_"):
			out = append(out, Span{ind, end, Comment})
		default:
			if n := listMarker(line); n > 0 {
				out = append(out, Span{ind, ind + n, Number})
				ind += n
			}
			out = lexInline(src, ind, end, out)
		}
		i = end + 1
	}
	return out
}

// lexInline appends the inline spans of src[i:end], a single line.
func lexInline(src []byte, i, end int, out []Span) []Span {
	for i < end {
		switch c := src[i]; {
		case c == '\\':
			i += 2
		case c == '`':
			n := run(src, i, end)
			close := bytes.Index(src[i+n:end], src[i:i+n])
			if close < 0 {
				i += n
				continue
			}
			out = append(out, Span{i, i + n + close + n, String})
			i += n + close + n
		case c == '*' || c == '_':
			n := min(run(src, i, end), 2)
			delim := src[i : i+n]
			close := bytes.Index(src[i+n:end], delim)
			intraword := c == '_' && i > 0 && isIdent(src[i-1])
			if intraword || close <= 0 || src[i+n] == ' ' || src[i+n+close-1] == ' ' {
				i += run(src, i, end)
				continue
			}
			kind := Emphasis
			if n == 2 {
				kind = Strong
			}
			out = append(out, Span{i, i + n + close + n, kind})
			i += n + close + n
		case c == '[':
			mid := bytes.Index(src[i:end], []byte("]("))
			if mid < 0 {
				i++
				continue
			}
			close := bytes.IndexByte(src[i+mid:end], ')')
			if close < 0 {
				i++
				continue
			}
			out = append(out, Span{i, i + mid + 1, Keyword}, Span{i + mid + 1, i + mid + close + 1, Comment})
			i += mid + close + 1
		default:
			i++
		}
	}
	return out
}

// isHeading reports an ATX heading: one to six '#' then a space or nothing.
func isHeading(line []byte) bool {
	n := run(line, 0, len(line))
	return len(line) > 0 && line[0] == '#' && n <= 6 && (n == len(line) || line[n] == ' ')
}

// onlyOf reports a line of at least three c, maybe spaced, as in rules.
func onlyOf(line []byte, c string) bool {
	rest := bytes.ReplaceAll(line, []byte(" "), nil)
	return len(rest) >= 3 && len(bytes.Trim(rest, c)) == 0
}

// listMarker returns the length of a list marker ("-", "12.") that a space
// follows, or 0.
func listMarker(line []byte) int {
	j := 0
	for j < len(line) && isDigit(line[j]) {
		j++
	}
	switch {
	case j == 0 && len(line) > 0 && (line[0] == '-' || line[0] == '*' || line[0] == '+'):
		j = 1
	case j > 0 && j < len(line) && (line[j] == '.' || line[j] == ')'):
		j++
	default:
		return 0
	}
	if j < len(line) && line[j] == ' ' {
		return j
	}
	return 0
}

// run returns the number of repeats of src[i] starting at i, before end.
func run(src []byte, i, end int) int {
	j := i
	for j < end && src[j] == src[i] {
		j++
	}
	return j - i
}
