package ui

import (
	"regexp"
	"strconv"
	"strings"
)

// sgrCarry tracks active SGR attributes across captured rows.
//
// tmux capture-pane -e emits colour/attribute codes only where they change
// relative to the running stream — a full-width background set on one row is
// not restated on the next. TerminalPane.View appends a hard reset to every
// rendered line (so styles can't bleed into borders), which destroys that
// implicit carry-over. This tracker replays the active state at the start of
// each subsequent line.
type sgrCarry struct {
	fg    string       // active foreground params ("31", "38;5;12", "38;2;r;g;b"), "" = default
	bg    string       // active background params, "" = default
	attrs map[int]bool // active attribute codes (1 bold, 2 dim, 3 italic, ...)
}

var sgrSeqRe = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

func newSGRCarry() *sgrCarry {
	return &sgrCarry{attrs: make(map[int]bool)}
}

// Prefix returns the escape sequence that restores the currently active
// state, or "" if everything is default.
func (c *sgrCarry) Prefix() string {
	var params []string
	for _, a := range []int{1, 2, 3, 4, 5, 7, 8, 9} {
		if c.attrs[a] {
			params = append(params, strconv.Itoa(a))
		}
	}
	if c.fg != "" {
		params = append(params, c.fg)
	}
	if c.bg != "" {
		params = append(params, c.bg)
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}

// Consume updates the state with every SGR sequence in the line.
func (c *sgrCarry) Consume(line string) {
	for _, m := range sgrSeqRe.FindAllStringSubmatch(line, -1) {
		c.apply(m[1])
	}
}

func (c *sgrCarry) apply(raw string) {
	if raw == "" {
		c.reset()
		return
	}
	// Colon sub-parameter form ("38:2:r:g:b") arrives as one token; treat it
	// like its semicolon equivalent by normalising for the parse only.
	toks := strings.Split(strings.ReplaceAll(raw, ":", ";"), ";")
	for i := 0; i < len(toks); i++ {
		n := atoi(toks[i])
		switch {
		case n == 0:
			c.reset()
		case n == 38 || n == 48:
			// Extended colour: 38;5;n or 38;2;r;g;b — consume the args.
			args := 0
			if i+1 < len(toks) {
				switch atoi(toks[i+1]) {
				case 5:
					args = 2
				case 2:
					args = 4
				}
			}
			if args == 0 || i+args >= len(toks) {
				return // malformed; drop the rest of this sequence
			}
			val := strings.Join(toks[i:i+args+1], ";")
			if n == 38 {
				c.fg = val
			} else {
				c.bg = val
			}
			i += args
		case (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
			c.fg = toks[i]
		case n == 39:
			c.fg = ""
		case (n >= 40 && n <= 47) || (n >= 100 && n <= 107):
			c.bg = toks[i]
		case n == 49:
			c.bg = ""
		case n >= 1 && n <= 9:
			c.attrs[n] = true
		case n == 22:
			delete(c.attrs, 1)
			delete(c.attrs, 2)
		case n == 23:
			delete(c.attrs, 3)
		case n == 24:
			delete(c.attrs, 4)
		case n == 25:
			delete(c.attrs, 5)
		case n == 27:
			delete(c.attrs, 7)
		case n == 28:
			delete(c.attrs, 8)
		case n == 29:
			delete(c.attrs, 9)
		}
	}
}

func (c *sgrCarry) reset() {
	c.fg = ""
	c.bg = ""
	c.attrs = make(map[int]bool)
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}
