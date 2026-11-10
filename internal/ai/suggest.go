package ai

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const (
	suggestMaxTokens   = 1024
	maxSuggestionWords = 60
)

//go:embed prompts/suggest_system.txt
var suggestSystemPrompt string

var contactLike = regexp.MustCompile(`(?i)https?://|www\.|\b[a-z0-9-]+\.(?:com|net|org|io|me|ly|co|app|link|gg|xyz|info|biz|us|uk|es|pe|mx|ar|de|fr)\b|[a-z0-9._%+-]+@[a-z0-9-]+\.|\+?\d[\d\s().-]{7,}\d`)

func HasContact(s string) bool {
	return contactLike.MatchString(s)
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
		q := in.Question
		if q == "" || HasContact(q) || len([]rune(q)) > 200 {
			q = "Can you make one of the dates, and which?"
		}
		when := ""
		if in.Options != "" && (in.Hours == "" || !strings.Contains(q, in.Hours)) {
			when = " The class runs " + in.Options + "."
		}
		return hi + q + when + " Answer on your CommitPool page."
	case "condition":
		return fmt.Sprintf("%s%s runs %s, and you said you could come %s. Is there any way you could make it?", hi, in.Date, in.Hours, in.Condition)
	case "reserve":
		return fmt.Sprintf("%sReservations for %s on %s are open until %s, and you can make it. Reserve your spot from the link you got when you joined. Nothing is charged unless %d people reserve.", hi, in.WorkshopTitle, in.Date, in.Deadline, in.Needed)
	case "share":
		return fmt.Sprintf("%s on %s needs %s by %s, or it won’t happen and nobody pays. Know someone who’d enjoy it? Please pass it on.", in.WorkshopTitle, in.Date, morePeople(in.Missing), in.Deadline)
	case "rescue":
		return fmt.Sprintf("%s on %s has more time: reservations now stay open until %s. It still needs %s, or it won’t happen and nobody pays. Know someone who’d enjoy it? Please pass it on.", in.WorkshopTitle, in.Date, in.Deadline, morePeople(in.Missing))
	}
	return ""
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
	if in.Kind != "share" && in.Kind != "rescue" {
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
	return sb.String()
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
	case len(strings.Fields(text)) > maxSuggestionWords:
		return "", meta, errors.New("ai: suggest: the answer was too long")
	case HasContact(text):
		return "", meta, errors.New("ai: suggest: the answer carried a link or contact")
	}
	return text, meta, nil
}
