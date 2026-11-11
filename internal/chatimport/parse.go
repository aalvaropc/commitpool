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

var (
	datedHeader = regexp.MustCompile(`^\[?\d{1,2}[./]\d{1,2}[./]\d{2,4},? \d{1,2}:\d{2}(:\d{2})?\s?([AaPp]\.?[Mm]\.?)?\]?\s?(-\s)?`)
	plainHeader = regexp.MustCompile(`^(\[?\d{1,2}:\d{2}\s?([AaPp][Mm])?\]?\s)?`)

	systemMessage = regexp.MustCompile(`(?is)^(` +
		`<media omitted>|` +
		`(image|video|audio|sticker|gif|document|contact card|voice call|video call) omitted|` +
		`this message was deleted\.?|you deleted this message\.?|` +
		`messages and calls are end-to-end encrypted.*|` +
		`(you )?(added|removed) .+|` +
		`(you )?(left|joined)|` +
		`(you )?(joined using this group.s invite link|joined using a group link|created group.*|created this group.*)|` +
		`(you )?changed (the )?(subject|group|this group|their phone number).*` +
		`)$`)

	systemLine = regexp.MustCompile(`(?is)^[^:]{1,80}? (` +
		`joined using this group.s invite link|joined using a group link|` +
		`created group.*|created this group.*|` +
		`changed (the )?(subject|group|this group|their phone number).*|` +
		`(added|removed) [^:]+|left|joined|` +
		`.*end-to-end encrypted.*` +
		`)$`)

	editedSuffix = regexp.MustCompile(`(?i)\s*<this message was edited>\s*$`)
)

func normalize(text string) string {
	text = strings.ToValidUTF8(text, "")
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\u200e', r == '\u200f', r == '\ufeff',
			r >= '\u202a' && r <= '\u202e',
			r >= '\u2066' && r <= '\u2069':
		case r == '\u202f', r == '\u00a0', r == '\t':
			b.WriteRune(' ')
		case r == '\u2028', r == '\u2029', r == '\u0085', r == '\r':
			b.WriteRune('\n')
		case r == '\n':
			b.WriteRune(r)
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitSender(rest string) (string, string, bool) {
	for i := 0; i < len(rest); i++ {
		if rest[i] != ':' {
			continue
		}
		if i+1 == len(rest) || rest[i+1] == ' ' {
			name := cleanName(rest[:i])
			if !validSender(name) {
				return "", "", false
			}
			return name, strings.TrimSpace(rest[i+1:]), true
		}
	}
	return "", "", false
}

type builder struct {
	people  []Person
	index   map[string]int
	skipped int
	tooMany bool
	name    string
	lines   []string
	open    bool
}

func (b *builder) flush() {
	if !b.open {
		return
	}
	b.open = false
	text := strings.TrimSpace(editedSuffix.ReplaceAllString(strings.Join(b.lines, "\n"), ""))
	name := b.name
	b.name, b.lines = "", nil
	if text == "" || systemMessage.MatchString(text) {
		b.skipped++
		return
	}
	key := fold(name)
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
	b.lines = []string{first}
}

func (b *builder) extend(line string) {
	if b.open && line != "" {
		b.lines = append(b.lines, line)
	}
}

func Parse(text string) (Chat, error) {
	clean := normalize(text)
	if strings.TrimSpace(clean) == "" {
		return Chat{}, ErrEmpty
	}
	if len(text) > MaxChars && utf8.RuneCountInString(text) > MaxChars {
		return Chat{}, ErrTooLong
	}
	lines := strings.Split(clean, "\n")
	dated := false
	for _, l := range lines {
		if datedHeader.MatchString(strings.TrimSpace(l)) {
			dated = true
			break
		}
	}
	b := &builder{index: map[string]int{}}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if dated {
			loc := datedHeader.FindStringIndex(line)
			if loc == nil {
				b.extend(line)
				continue
			}
			rest := strings.TrimSpace(line[loc[1]:])
			if systemLine.MatchString(rest) {
				b.flush()
				b.skipped++
				continue
			}
			name, msg, ok := splitSender(rest)
			if !ok {
				b.flush()
				b.skipped++
				continue
			}
			b.start(name, msg)
			continue
		}
		rest := strings.TrimSpace(line[plainHeader.FindStringIndex(line)[1]:])
		if name, msg, ok := splitSender(rest); ok && !systemLine.MatchString(rest) {
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
