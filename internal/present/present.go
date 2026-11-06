package present

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const dayLayout = "Mon, Jan 2"

func ZoneName(loc *time.Location) string {
	name := loc.String()
	if fixed, ok := strings.CutPrefix(name, "Etc/"); ok {
		return fixedOffsetName(fixed)
	}
	slash := strings.LastIndexByte(name, '/')
	if slash < 0 {
		return name
	}
	return strings.ReplaceAll(name[slash+1:], "_", " ") + " time"
}

func fixedOffsetName(name string) string {
	hours, ok := strings.CutPrefix(name, "GMT")
	if !ok || hours == "" || hours == "0" || hours == "+0" || hours == "-0" {
		return "UTC"
	}
	switch hours[0] {
	case '+':
		return "UTC−" + hours[1:]
	case '-':
		return "UTC+" + hours[1:]
	}
	return "UTC"
}

func Clock(t time.Time) string {
	if t.Minute() == 0 {
		return t.Format("3 PM")
	}
	return t.Format("3:04 PM")
}

func Day(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(dayLayout)
}

func DateLabel(start, end time.Time, loc *time.Location) string {
	s, e := start.In(loc), end.In(loc)
	if s.Year() == e.Year() && s.YearDay() == e.YearDay() {
		return fmt.Sprintf("%s, %s–%s (%s)", s.Format(dayLayout), Clock(s), Clock(e), ZoneName(loc))
	}
	return fmt.Sprintf("%s, %s – %s, %s (%s)", s.Format(dayLayout), Clock(s), e.Format(dayLayout), Clock(e), ZoneName(loc))
}

func Moment(t time.Time, loc *time.Location) string {
	at := t.In(loc)
	return fmt.Sprintf("%s at %s (%s)", at.Format(dayLayout), Clock(at), ZoneName(loc))
}

func Price(m domain.Money) string {
	if m.Currency != "USD" {
		return m.String() + " " + m.Currency
	}
	whole := groupThousands(m.Minor / 100)
	if cents := m.Minor % 100; cents != 0 {
		return fmt.Sprintf("$%s.%02d", whole, cents)
	}
	return "$" + whole
}

func groupThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
