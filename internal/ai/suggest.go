package ai

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const (
	suggestMaxTokens   = 1024
	maxSuggestionWords = 60
	maxQuestionRunes   = 200
	groupReplyKind     = "group_reply"
	defaultQuestion    = "Can you make one of the dates, and which?"
)

//go:embed prompts/suggest_system.txt
var suggestSystemPrompt string

var contactLike = regexp.MustCompile(`(?i)https?://|www\.|\b[a-z0-9-]+\.(?:com|net|org|io|me|ly|co|app|link|gg|xyz|info|biz|us|uk|es|pe|mx|ar|de|fr)\b|[a-z0-9._%+-]+@[a-z0-9-]+\.|\+?\d[\d\s().-]{7,}\d`)

func HasContact(s string) bool {
	return contactLike.MatchString(s)
}

var amountLike = regexp.MustCompile(`(?i)[$€£¥]\s?\d[\d,]*(?:\.\d+)?|\b(?:usd|eur|gbp)\s?\d[\d,]*(?:\.\d+)?|\b\d[\d,]*(?:\.\d+)?\s?(?:[$€£¥]|(?:usd|eur|gbp|dollars?|euros?|pounds?|bucks)\b)`)

func amounts(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range amountLike.FindAllString(s, -1) {
		digits := strings.Map(func(r rune) rune {
			if r == '.' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, m)
		if f, err := strconv.ParseFloat(digits, 64); err == nil {
			digits = strconv.FormatFloat(f, 'f', -1, 64)
		}
		out[digits] = true
	}
	return out
}

func namesAnAmountNotIn(text, facts string) bool {
	given := amounts(facts)
	for a := range amounts(text) {
		if !given[a] {
			return true
		}
	}
	return false
}

type NamedQuestion struct {
	Name     string
	Question string
}

func askable(q string) string {
	if q == "" || HasContact(q) || len([]rune(q)) > maxQuestionRunes {
		return defaultQuestion
	}
	return q
}

func WithoutQuestions(text string, in SuggestInput) string {
	for _, q := range in.Questions {
		ask := strings.Join(strings.Fields(askable(q.Question)), " ")
		var sb strings.Builder
		for i := 0; i < len(text); {
			if end := i + len(ask); end <= len(text) && strings.EqualFold(text[i:end], ask) {
				i = end
				continue
			}
			_, size := utf8.DecodeRuneInString(text[i:])
			sb.WriteString(text[i : i+size])
			i += size
		}
		text = sb.String()
	}
	return text
}

type SuggestInput struct {
	WorkshopTitle string
	Kind          string
	Name          string
	Date          string
	Hours         string
	Deadline      string
	Earlier       string
	Needed        int
	Missing       int
	Question      string
	Condition     string
	Options       string
	CanCome       int
	Tied          []string
	Questions     []NamedQuestion
}

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return "there"
}

func PlainSuggestion(in SuggestInput) string {
	hi := "Hi " + firstName(in.Name) + "! "
	switch in.Kind {
	case "clarify":
		q := askable(in.Question)
		when := ""
		if in.Options != "" && (in.Hours == "" || !strings.Contains(q, in.Hours)) {
			when = " The class runs " + in.Options + "."
		}
		return hi + q + when + " Answer on your CommitPool page."
	case "condition":
		return fmt.Sprintf("%s%s runs %s, and you said you could come %s. Could that conflict move so you can make it?", hi, in.Date, in.Hours, in.Condition)
	case "reserve":
		return fmt.Sprintf("%sReservations for %s on %s are open until %s, and you can make it. Reserve your spot from the link you got when you joined. Nothing is charged unless %d people reserve.", hi, in.WorkshopTitle, in.Date, in.Deadline, in.Needed)
	case "share":
		return fmt.Sprintf("%s on %s needs %s by %s, or it won’t happen and nobody pays. Know someone who’d enjoy it? Please pass it on.", in.WorkshopTitle, in.Date, morePeople(in.Missing), in.Deadline)
	case "rescue":
		return fmt.Sprintf("%s on %s has more time: reservations now stay open until %s. It still needs %s, or it won’t happen and nobody pays. Know someone who’d enjoy it? Please pass it on.", in.WorkshopTitle, in.Date, in.Deadline, morePeople(in.Missing))
	case groupReplyKind:
		return plainGroupReply(in)
	}
	return ""
}

func plainGroupReply(in SuggestInput) string {
	var sb strings.Builder
	sb.WriteString(in.WorkshopTitle + ": thanks for answering, everyone! ")
	switch {
	case in.CanCome == 0:
		sb.WriteString("No date works yet for those who answered.")
	case len(in.Tied) > 1:
		fmt.Fprintf(&sb, "%s are tied: each works for %d of you so far", joinNames(in.Tied), in.CanCome)
	default:
		fmt.Fprintf(&sb, "%s works for %d of you so far", in.Date, in.CanCome)
	}
	if in.CanCome > 0 && in.Missing == 0 && in.Needed > 0 {
		fmt.Fprintf(&sb, ", the %d the class needs", in.Needed)
	}
	if in.CanCome > 0 && len(in.Tied) > 1 {
		fmt.Fprintf(&sb, ", and %s comes first", in.Tied[0])
	}
	if in.CanCome > 0 {
		sb.WriteString(".")
	}
	switch {
	case in.CanCome == 0 && in.Needed > 0:
		fmt.Fprintf(&sb, " The class needs %s.", people(in.Needed))
	case in.Missing > 0:
		fmt.Fprintf(&sb, " The class needs %s.", morePeople(in.Missing))
	}
	for _, q := range in.Questions {
		fmt.Fprintf(&sb, " %s: %s", firstName(q.Name), askable(q.Question))
	}
	if len(in.Questions) > 0 {
		sb.WriteString(" Answer on your CommitPool page.")
	}
	sb.WriteString(" Anyone who hasn’t answered yet: say when you can come.")
	return sb.String()
}

func morePeople(n int) string {
	if n == 1 {
		return "1 more person"
	}
	return fmt.Sprintf("%d more people", n)
}

func SuggestFacts(in SuggestInput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Kind of message: %s\n", in.Kind)
	fmt.Fprintf(&sb, "Workshop title: %s\n", jsonString(in.WorkshopTitle))
	if in.Kind != "share" && in.Kind != "rescue" && in.Kind != groupReplyKind {
		fmt.Fprintf(&sb, "Person: %s\n", jsonString(in.Name))
	}
	fmt.Fprintf(&sb, "Date: %s\n", jsonString(in.Date))
	if in.Hours != "" {
		fmt.Fprintf(&sb, "Hours: %s\n", jsonString(in.Hours))
	}
	if in.Deadline != "" {
		fmt.Fprintf(&sb, "Reservations close: %s\n", jsonString(in.Deadline))
	}
	if in.Earlier != "" {
		fmt.Fprintf(&sb, "Earlier deadline, before the organizer extended it: %s\n", jsonString(in.Earlier))
	}
	if in.Needed > 0 {
		fmt.Fprintf(&sb, "People needed for the class to happen: %d\n", in.Needed)
	}
	if in.Missing > 0 {
		fmt.Fprintf(&sb, "More people needed: %d\n", in.Missing)
	}
	if in.Options != "" {
		fmt.Fprintf(&sb, "Dates and hours the class can run: %s\n", jsonString(in.Options))
	}
	if in.Question != "" {
		fmt.Fprintf(&sb, "Open question: %s\n", jsonString(in.Question))
	}
	if in.Condition != "" {
		fmt.Fprintf(&sb, "The person's condition: %s\n", jsonString(in.Condition))
	}
	if in.Kind == groupReplyKind {
		groupReplyFacts(&sb, in)
	}
	return sb.String()
}

func groupReplyFacts(sb *strings.Builder, in SuggestInput) {
	if len(in.Tied) > 1 {
		fmt.Fprintf(sb, "Tied dates, earliest first: %s (%s comes first)\n", jsonList(in.Tied), jsonString(in.Tied[0]))
		fmt.Fprintf(sb, "People who can come on each tied date so far: %d\n", in.CanCome)
	} else {
		fmt.Fprintf(sb, "People who can come on that date so far: %d\n", in.CanCome)
	}
	for _, q := range in.Questions {
		fmt.Fprintf(sb, "Open question for %s: %s\n", jsonString(q.Name), jsonString(askable(q.Question)))
	}
}

func wordLimit(in SuggestInput) int {
	limit := maxSuggestionWords
	for _, q := range in.Questions {
		limit += 1 + len(strings.Fields(askable(q.Question)))
	}
	return limit
}

func (Fake) Suggest(_ context.Context, in SuggestInput) (string, Meta, error) {
	return PlainSuggestion(in), Meta{Provider: fakeProvider, Model: fakeModel}, nil
}

func (b *Bedrock) Suggest(ctx context.Context, in SuggestInput) (string, Meta, error) {
	msg, meta, err := b.send(ctx, "suggest", anthropic.BetaMessageNewParams{
		Model:        b.model,
		MaxTokens:    suggestMaxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: suggestSystemPrompt}},
		Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(SuggestFacts(in)))},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: b.effort},
	})
	if err != nil {
		return "", meta, err
	}
	text := strings.Join(strings.Fields(answerText(msg)), " ")
	switch {
	case text == "":
		return "", meta, errors.New("ai: suggest: the answer was empty")
	case len(strings.Fields(text)) > wordLimit(in):
		return "", meta, errors.New("ai: suggest: the answer was too long")
	case HasContact(text):
		return "", meta, errors.New("ai: suggest: the answer carried a link or contact")
	case namesAnAmountNotIn(text, SuggestFacts(in)):
		return "", meta, errors.New("ai: suggest: the answer named an amount the facts do not give")
	case len(in.Tied) > 1 && strings.Contains(strings.ToLower(text), "recommend"):
		return "", meta, errors.New("ai: suggest: the answer called a tied date recommended")
	}
	return text, meta, nil
}
