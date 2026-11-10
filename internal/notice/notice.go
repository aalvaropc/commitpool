package notice

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

type Kind string

const (
	Joined          Kind = "joined"
	Reserved        Kind = "reserved"
	Confirmed       Kind = "confirmed"
	PaymentFailed   Kind = "payment_failed"
	RefundCompleted Kind = "refund_completed"
	Cancelled       Kind = "cancelled"
	OwnerCreated    Kind = "owner_created"
	OwnerConfirmed  Kind = "owner_confirmed"
	OwnerCancelled  Kind = "owner_cancelled"
)

const DeadlineExtended Kind = "deadline_extended"

var Kinds = []Kind{Joined, Reserved, Confirmed, PaymentFailed, RefundCompleted, Cancelled, OwnerCreated, OwnerConfirmed, OwnerCancelled, DeadlineExtended}

type Data struct {
	Name          string
	Title         string
	Host          string
	Place         string
	When          string
	Price         string
	Needed        int
	Paid          int
	Collected     string
	Deadline      string
	Reason        string
	Opened        bool
	Closed        bool
	Link          string
	InviteLink    string
	InviteMessage string
	Earlier       string
}

type Email struct {
	Subject, Text, HTML string
}

type fact struct{ Label, Value string }

type action struct{ Label, URL string }

type message struct {
	Subject    string
	Preheader  string
	Headline   string
	Paragraphs []string
	Quote      string
	Facts      []fact
	Action     *action
	After      []string
	Share      string
	Why        string
}

var (
	//go:embed templates
	files embed.FS
	html  = htmltemplate.Must(htmltemplate.ParseFS(files, "templates/email.html.tmpl"))
	text  = texttemplate.Must(texttemplate.ParseFS(files, "templates/email.txt.tmpl"))
)

func Render(kind Kind, d Data) (Email, error) {
	m, err := compose(kind, d)
	if err != nil {
		return Email{}, err
	}
	m.Subject = oneLine(m.Subject)
	var t, h bytes.Buffer
	if err := text.Execute(&t, m); err != nil {
		return Email{}, fmt.Errorf("notice: %s text: %w", kind, err)
	}
	if err := html.Execute(&h, m); err != nil {
		return Email{}, fmt.Errorf("notice: %s html: %w", kind, err)
	}
	return Email{Subject: m.Subject, Text: t.String(), HTML: h.String()}, nil
}

func InviteMessage(title, place, price string, needed int, link string) string {
	where := ""
	if place != "" {
		where = " at " + place
	}
	who := fmt.Sprintf("%d people reserve", needed)
	if needed == 1 {
		who = "1 person reserves"
	}
	return fmt.Sprintf("I’m planning %s%s. It’s %s a spot, and it only happens if %s; otherwise nobody pays. Tell me when you can come: %s", title, where, price, who, link)
}

func CarriesPrivateLink(kind Kind) bool {
	return kind == Joined || kind == OwnerCreated
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func ownerWontHappen(d Data) string {
	if d.Closed {
		return "Not enough people reserved by " + d.Deadline + ", so " + d.Title + " won’t happen."
	}
	return d.Title + " won’t happen."
}

func cancelledMoney(opened bool, moved string) string {
	if !opened {
		return "Nobody had reserved, so nothing was held or charged."
	}
	return moved
}

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return "there"
}

func (d Data) facts(labels ...string) []fact {
	values := map[string]string{"When": d.When, "Where": d.Place, "With": d.Host, "Price": d.Price}
	var out []fact
	for _, l := range labels {
		if v := values[l]; v != "" {
			out = append(out, fact{l, v})
		}
	}
	return out
}

func compose(kind Kind, d Data) (message, error) {
	hi := "Hi " + firstName(d.Name) + ","
	joined := "You get this because you left your email when you joined " + d.Title + " on CommitPool."
	created := "You get this because you left your email when you created " + d.Title + " on CommitPool."
	people := fmt.Sprintf("%d people", d.Needed)
	if d.Needed == 1 {
		people = "1 person"
	}
	said, youSaid := "", ""
	if d.Reason != "" {
		said = "The organizer said: “" + d.Reason + "”"
		youSaid = "You said: “" + d.Reason + "”"
	}
	wontHappen := "The class won’t happen."
	if d.Closed {
		wontHappen = "Not enough people reserved by " + d.Deadline + ", so the class won’t happen."
	}
	paidPeople := fmt.Sprintf("%d people", d.Paid)
	if d.Paid == 1 {
		paidPeople = "1 person"
	}
	switch kind {
	case Joined:
		return message{
			Subject:    "Your spot link for " + d.Title,
			Preheader:  "Say when you can come, and reserve once the date is set.",
			Headline:   "You’re on the list for " + d.Title,
			Paragraphs: []string{hi, "This private link is yours. It’s where you say when you can come and, once the date is set, reserve your spot."},
			Facts:      d.facts("With", "Where", "Price"),
			Action:     &action{"Open your page", d.Link},
			After:      []string{"Keep it to yourself: anyone with it can act as you. Nothing is charged until enough people reserve."},
			Why:        joined,
		}, nil
	case Reserved:
		after := []string{"Your spot is on the private link you got when you joined."}
		return message{
			Subject:    "Spot reserved for " + d.Title + " — not charged yet",
			Preheader:  "Nothing is charged unless enough people reserve.",
			Headline:   "Your spot is reserved",
			Paragraphs: []string{hi, "PayPal is holding " + d.Price + " for " + d.Title + ". Nothing is charged yet.", "If " + people + " reserve by " + d.Deadline + ", you’re charged and the class is on. If not, the hold is released and you pay nothing."},
			Facts:      d.facts("When", "Where", "Price"),
			After:      after,
			Why:        joined,
		}, nil
	case Confirmed:
		return message{
			Subject:    "It’s on: " + d.Title,
			Preheader:  "Enough people reserved. See you there.",
			Headline:   "It’s on!",
			Paragraphs: []string{hi, "Enough people reserved, so " + d.Title + " is happening. Your payment of " + d.Price + " went through."},
			Facts:      d.facts("When", "Where", "With"),
			After:      []string{"The calendar invite is attached. Your spot is on the private link you got when you joined."},
			Why:        joined,
		}, nil
	case PaymentFailed:
		return message{
			Subject:    "Your payment for " + d.Title + " did not go through",
			Preheader:  "Nothing was charged.",
			Headline:   "Your payment didn’t go through",
			Paragraphs: []string{hi, "PayPal could not charge the " + d.Price + " it was holding for " + d.Title + ", so nothing was charged and the hold is released.", "While reservations are open, you can reserve again from the private link you got when you joined."},
			Why:        joined,
		}, nil
	case RefundCompleted:
		return message{
			Subject:    "Your refund for " + d.Title + " is complete",
			Preheader:  "PayPal returned your payment.",
			Headline:   "Refund complete",
			Paragraphs: []string{hi, "PayPal has refunded your " + d.Price + " for " + d.Title + ". It can take a few days to show in your account."},
			Why:        joined,
		}, nil
	case Cancelled:
		return message{
			Subject:    d.Title + " is cancelled",
			Preheader:  "Any hold is released and any payment refunded.",
			Headline:   d.Title + " is cancelled",
			Paragraphs: []string{hi, wontHappen},
			Quote:      said,
			After:      []string{cancelledMoney(d.Opened, "If PayPal was holding money for you, the hold is released. If you were charged, the refund is on its way and can take a few days; you can follow it on the private link you got when you joined.")},
			Why:        joined,
		}, nil
	case OwnerCreated:
		return message{
			Subject:    "Your organizer link for " + d.Title,
			Preheader:  "Keep this email: it’s the only way back to your workshop page.",
			Headline:   d.Title + " is ready",
			Paragraphs: []string{hi, "This is your private organizer link. It’s the only way back to your workshop page, so keep this email and don’t forward it."},
			Action:     &action{"Open your organizer page", d.Link},
			After:      []string{"Paste this in your group chat to invite people:"},
			Share:      d.InviteMessage,
			Why:        created,
		}, nil
	case OwnerConfirmed:
		return message{
			Subject:    "It’s on: " + d.Title,
			Preheader:  fmt.Sprintf("%d paid, %s collected.", d.Paid, d.Collected),
			Headline:   fmt.Sprintf("It’s on — %d paid", d.Paid),
			Paragraphs: []string{hi, fmt.Sprintf("%s paid %s in total for %s. That’s enough to run it.", paidPeople, d.Collected, d.Title)},
			Facts:      d.facts("When", "Where"),
			After:      []string{"Your organizer link shows who paid. Spots still open can be reserved until reservations close."},
			Why:        created,
		}, nil
	case DeadlineExtended:
		return message{
			Subject:    "More time to reserve for " + d.Title,
			Preheader:  "Reservations now close " + d.Deadline + ".",
			Headline:   "Reservations now close " + d.Deadline,
			Paragraphs: []string{hi, "The organizer moved the deadline for " + d.Title + " from " + d.Earlier + " to " + d.Deadline + ", so more people have time to reserve. It can move only once.", "If " + people + " reserve by then, holds are charged and the class is on. If not, every hold is released and nobody pays."},
			Facts:      d.facts("When", "Where", "Price"),
			After:      []string{"Your spot stays as it is. Until charging starts, you can still leave and have any hold released from the private link you got when you joined."},
			Why:        joined,
		}, nil
	case OwnerCancelled:
		return message{
			Subject:    d.Title + " is cancelled",
			Preheader:  "Every hold is released and every payment refunded.",
			Headline:   d.Title + " is cancelled",
			Paragraphs: []string{hi, ownerWontHappen(d)},
			Quote:      youSaid,
			After:      []string{cancelledMoney(d.Opened, "Every hold is released and every payment refunded through PayPal. Your organizer link shows each one as it completes.")},
			Why:        created,
		}, nil
	}
	return message{}, fmt.Errorf("notice: unknown kind %q", kind)
}
