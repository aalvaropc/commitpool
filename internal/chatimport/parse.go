package chatimport

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxChars     = 20000
	MaxPeople    = 30
	MaxTextRunes = 2000
)

var (
	ErrEmpty         = errors.New("the pasted chat is empty")
	ErrTooLong       = errors.New("the pasted chat is longer than 20000 characters")
	ErrTooManyPeople = errors.New("the pasted chat has more than 30 people")
	ErrNoLines       = errors.New("no lines in the form Name: message were found")
)

type Person struct {
	Name     string
	Messages []string
}

type Chat struct {
	People  []Person
	Skipped int
}

func (p Person) Text(limit int) string {
	if limit <= 0 || len(p.Messages) == 0 {
		return ""
	}
	used := 0
	start := len(p.Messages)
	for i := len(p.Messages) - 1; i >= 0; i-- {
		n := utf8.RuneCountInString(p.Messages[i])
		if used > 0 {
			n++
		}
		if used+n > limit {
			break
		}
		used += n
		start = i
	}
	if start == len(p.Messages) {
		r := []rune(p.Messages[len(p.Messages)-1])
		return string(r[len(r)-limit:])
	}
	return strings.Join(p.Messages[start:], "\n")
}

const groupEventMark = "\u200e"

var (
	datedHeader   = regexp.MustCompile(`^\[?\d{1,4}[./-]\d{1,2}[./-]\d{1,4},?\s(?:\p{L}{1,3}\s)?\d{1,2}[:.]\d{2}(?:[:.]\d{2})?(?:\s?[AaPp]\.?\s?[Mm]\.?)?(?:\]\s?|\s-\s)`)
	webCopyHeader = regexp.MustCompile(`^\[\d{1,2}[:.]\d{2}(?:\s?[AaPp]\.?\s?[Mm]\.?)?,\s\d{1,4}[./-]\d{1,2}[./-]\d{1,4}\]\s`)
	plainHeader   = regexp.MustCompile(`^(\[?\d{1,2}:\d{2}\s?([AaPp][Mm])?\]?\s)?`)

	mediaKinds = `image|video|audio|sticker|gif|document|contact card|voice message|view once voice message|view once photo|view once video`

	systemMessage = regexp.MustCompile(`(?i)^(` +
		`<media omitted>|` +
		`(?:.+ \x{2022} \d+ pages? )?(?:` + mediaKinds + `) omitted|` +
		`null|` +
		`.+ \(file attached\)|` +
		`<attached: .+>|` +
		`location: https?://\S+|` +
		`missed (?:group )?(?:voice|video) call(?:, tap to call back)?\.?|` +
		`this message was deleted\.?|you deleted this message\.?|` +
		`this message was deleted by admin .+|` +
		`waiting for this message.*|` +
		`messages and calls are end-to-end encrypted\.(?: no one outside of this chat.*)?` +
		`)$`)

	encryptionNotice = regexp.MustCompile(`(?i)^messages and calls are end-to-end encrypted`)

	systemLine = regexp.MustCompile(`(?i)^[^:]{1,80}? (?:` +
		`joined using this group.s invite link|joined using a group link|` +
		`created (?:group|this group) .*|` +
		`changed the subject (?:from .* )?to .*|` +
		`changed (?:this group.s|the group.s) .*|` +
		`changed the group description|changed their phone number to .*|` +
		`(?:added|removed) [^:]+|left|joined` +
		`)$`)

	editedSuffix = regexp.MustCompile(`(?i)\s*<this message was edited>\s*$`)
)

func headerEnd(line string) int {
	if loc := datedHeader.FindStringIndex(line); loc != nil {
		return loc[1]
	}
	if loc := webCopyHeader.FindStringIndex(line); loc != nil {
		return loc[1]
	}
	return -1
}

func normalize(text string) string {
	text = strings.ToValidUTF8(text, "")
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\u200f', r == '\ufeff', r == '\u200b', r == '\u2060', r == '\u061c',
			r >= '\u202a' && r <= '\u202e',
			r >= '\u2066' && r <= '\u2069',
			r >= '\U000e0000' && r <= '\U000e007f':
		case r == '\u202f', r == '\u00a0', r == '\t':
			b.WriteRune(' ')
		case r == '\u2028', r == '\u2029', r == '\u0085', r == '\r':
			b.WriteRune('\n')
		case r == '\n', r == '\u200e':
			b.WriteRune(r)
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nameOnlyText(name string) bool {
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsMark(r) && r != ' ' && r != '\'' && r != '-' && r != '.' {
			return false
		}
	}
	return name != ""
}

func splitSender(rest string, plain bool) (string, string, bool) {
	for i := 0; i < len(rest); i++ {
		if rest[i] != ':' {
			continue
		}
		spaced := i+1 == len(rest) || rest[i+1] == ' '
		if !spaced {
			next, _ := utf8.DecodeRuneInString(rest[i+1:])
			if !unicode.IsLetter(next) || !nameOnlyText(rest[:i]) {
				continue
			}
		}
		name := cleanName(rest[:i])
		if !validSender(name) || (plain && !plainSender(name)) {
			return "", "", false
		}
		return name, strings.TrimSpace(rest[i+1:]), true
	}
	return "", "", false
}

type builder struct {
	people   []Person
	index    map[string]int
	chatName string
	skipped  int
	tooMany  bool
	name     string
	lines    []string
	marked   bool
	open     bool
}

func (b *builder) flush() {
	if !b.open {
		return
	}
	b.open = false
	name, marked := b.name, b.marked
	text := strings.TrimSpace(editedSuffix.ReplaceAllString(strings.Join(b.lines, "\n"), ""))
	b.name, b.lines, b.marked = "", nil, false
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	key := fold(name)
	if marked && encryptionNotice.MatchString(first) && b.chatName == "" {
		b.chatName = key
	}
	if text == "" || marked || systemMessage.MatchString(first) || isBot(name) || (b.chatName != "" && key == b.chatName) {
		b.skipped++
		return
	}
	i, ok := b.index[key]
	if !ok {
		if len(b.people) >= MaxPeople {
			b.tooMany = true
			return
		}
		i = len(b.people)
		b.index[key] = i
		b.people = append(b.people, Person{Name: name})
	}
	b.people[i].Messages = append(b.people[i].Messages, text)
}

func (b *builder) start(name, first string) {
	b.flush()
	b.open = true
	b.name = name
	b.marked = strings.HasPrefix(first, groupEventMark)
	b.lines = []string{strings.ReplaceAll(first, groupEventMark, "")}
}

func (b *builder) extend(line string) {
	line = strings.TrimSpace(strings.ReplaceAll(line, groupEventMark, ""))
	if b.open && line != "" {
		b.lines = append(b.lines, line)
	}
}

func cleanLine(raw string) string {
	return strings.TrimSpace(strings.TrimLeft(raw, groupEventMark+" "))
}

func Parse(text string) (Chat, error) {
	if len(text) > 4*MaxChars || utf8.RuneCountInString(text) > MaxChars {
		return Chat{}, ErrTooLong
	}
	clean := normalize(text)
	if strings.TrimSpace(strings.ReplaceAll(clean, groupEventMark, "")) == "" {
		return Chat{}, ErrEmpty
	}
	lines := strings.Split(clean, "\n")
	dated := false
	for _, raw := range lines {
		line := cleanLine(raw)
		if end := headerEnd(line); end >= 0 {
			if _, _, ok := splitSender(strings.TrimSpace(line[end:]), false); ok {
				dated = true
				break
			}
		}
	}
	b := &builder{index: map[string]int{}}
	for _, raw := range lines {
		line := cleanLine(raw)
		if line == "" {
			continue
		}
		if dated {
			end := headerEnd(line)
			if end < 0 {
				if name, msg, ok := splitSender(line, false); ok && b.open && fold(name) == fold(b.name) {
					b.start(name, msg)
					continue
				}
				b.extend(line)
				continue
			}
			rest := strings.TrimSpace(line[end:])
			if systemLine.MatchString(rest) {
				b.flush()
				b.skipped++
				continue
			}
			name, msg, ok := splitSender(rest, false)
			if !ok {
				b.flush()
				b.skipped++
				continue
			}
			b.start(name, msg)
			continue
		}
		rest := strings.TrimSpace(line[plainHeader.FindStringIndex(line)[1]:])
		if name, msg, ok := splitSender(rest, true); ok {
			b.start(name, msg)
			continue
		}
		b.extend(line)
	}
	b.flush()
	if b.tooMany {
		return Chat{}, ErrTooManyPeople
	}
	if len(b.people) == 0 {
		return Chat{}, ErrNoLines
	}
	return Chat{People: b.people, Skipped: b.skipped}, nil
}
