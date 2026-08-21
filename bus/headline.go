package bus

import "strings"

// MaxHeadline caps how much of a headline reaches a digest or listing.
const MaxHeadline = 160

// ShortHeadline reduces a headline to the single short line that listings
// promise. Headlines are meant to be one line; senders do paste whole
// multi-paragraph reports into the field, and one of those costs a reader more
// context than every other message in the digest combined. The full text is
// always a `hive bus read <id>` away.
func (a Announcement) ShortHeadline() string {
	headline, cut := a.Headline, false
	if i := strings.IndexAny(headline, "\r\n"); i >= 0 {
		headline, cut = headline[:i], true
	}
	if r := []rune(headline); len(r) > MaxHeadline {
		headline, cut = string(r[:MaxHeadline]), true
	}
	headline = strings.TrimRight(headline, " \t")
	if cut {
		headline += "…"
	}
	return headline
}

// foldHeadline moves anything past the first line of a headline, or past
// MaxHeadline characters of it, down into the body.
//
// Headlines are how listeners self-filter: the digest shows them and nothing
// else, so a headline carrying a whole report costs every reader more context
// than the rest of the digest combined. Enforcing it at the write path rather
// than only at render time means the log itself stays scannable, and nothing
// is lost — the overflow lands in the body, which is where it was always
// meant to go. A trailing ellipsis marks a headline that continues below.
func foldHeadline(msg Announcement) Announcement {
	headline := strings.TrimSpace(msg.Headline)
	overflow := ""

	if i := strings.IndexAny(headline, "\r\n"); i >= 0 {
		overflow = strings.TrimSpace(headline[i:])
		headline = strings.TrimRight(headline[:i], " \t")
	}

	if r := []rune(headline); len(r) > MaxHeadline {
		cut := MaxHeadline - 1 // leave room for the ellipsis
		// Prefer a word boundary, but only a reasonably close one — breaking
		// a 160-character headline at column 12 helps nobody.
		for i := cut; i > MaxHeadline/2; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		tail := strings.TrimSpace(string(r[cut:]))
		if overflow != "" {
			tail += "\n\n" + overflow
		}
		overflow = tail
		headline = strings.TrimRight(string(r[:cut]), " ")
	}

	if overflow == "" {
		msg.Headline = headline
		return msg
	}

	msg.Headline = headline + "\u2026"
	if body := strings.TrimSpace(msg.Body); body != "" {
		msg.Body = overflow + "\n\n" + body
	} else {
		msg.Body = overflow
	}
	return msg
}
