package invite

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/ics"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

func Terms(status domain.WorkshopStatus, price string, needed, paid int, loc *time.Location) string {
	terms := fmt.Sprintf("%s a spot; it happens if %d reserve, and nobody pays otherwise.", price, needed)
	switch status {
	case domain.WorkshopConfirmed:
		terms = fmt.Sprintf("It’s on: %d paid %s a spot.", paid, price)
	case domain.WorkshopCancelled:
		terms = "This class is cancelled; nobody pays for it."
	}
	return terms + " Times in " + present.ZoneName(loc) + "."
}

func Describe(w store.WorkshopRow, terms string) string {
	var lead []string
	if w.HostName != "" {
		lead = append(lead, "With "+w.HostName)
	}
	if w.Location != "" {
		lead = append(lead, w.Location)
	}
	if len(lead) == 0 {
		return terms
	}
	return strings.Join(lead, " · ") + ". " + terms
}

func Event(w store.WorkshopRow, p *store.ProposalRow, dates []store.CandidateDateRow, description, link string, now time.Time) (ics.Event, bool) {
	if p == nil {
		return ics.Event{}, false
	}
	var start, end time.Time
	for _, d := range dates {
		if d.ID == p.ChosenDateID {
			start, end = d.StartsAt, d.EndsAt
		}
	}
	if start.IsZero() {
		return ics.Event{}, false
	}
	status, step := ics.Tentative, 0
	switch w.Status {
	case domain.WorkshopConfirmed:
		status, step = ics.Confirmed, 1
	case domain.WorkshopCancelled:
		status, step = ics.Cancelled, 2
	}
	event := ics.Event{
		UID:         "workshop-" + w.ID + "@commitpool",
		Sequence:    p.Version*10 + step,
		Stamp:       now,
		Start:       start,
		End:         end,
		Summary:     w.Title,
		Description: description,
		Location:    w.Location,
		Status:      status,
	}
	if link != "" {
		event.Description += "\nInvite: " + link
		event.URL = link
	}
	return event, true
}

var slugBreak = regexp.MustCompile(`[^a-z0-9]+`)

func FileName(title string) string {
	slug := strings.Trim(slugBreak.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	if slug == "" {
		slug = "workshop"
	}
	return slug + ".ics"
}
