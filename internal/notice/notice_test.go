package notice_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/notice"
)

var update = flag.Bool("update", false, "rewrite the golden emails")

func sampleData() notice.Data {
	return notice.Data{
		Name:          "Priya Raman",
		Title:         "Life drawing for beginners",
		Host:          "Iris Calder",
		Place:         "The Long Room · Red Hook, Brooklyn",
		When:          "Sat, Nov 14, 10 AM–12 PM (New York time)",
		Price:         "$45",
		Needed:        3,
		Paid:          4,
		Collected:     "$180",
		Deadline:      "Fri, Nov 13 at 10 AM (New York time)",
		Link:          "https://example.test/p/PRIVATE",
		InviteLink:    "https://example.test/w/INVITE",
		InviteMessage: "I’m planning Life drawing for beginners. Tell me when you can come: https://example.test/w/INVITE",
	}
}

func TestRender_EveryKindMatchesItsGolden(t *testing.T) {
	for _, kind := range notice.Kinds {
		t.Run(string(kind), func(t *testing.T) {
			d := sampleData()
			if kind == notice.Cancelled || kind == notice.OwnerCancelled {
				d.Reason = "The room flooded."
				d.Opened = true
			}
			if kind == notice.OwnerCreated {
				d.Name = "Iris Calder"
				d.Link = "https://example.test/owner/ORGANIZER"
			}
			e, err := notice.Render(kind, d)
			if err != nil {
				t.Fatal(err)
			}
			got := "Subject: " + e.Subject + "\n\n" + e.Text + "\n---\n" + e.HTML
			path := filepath.Join("testdata", string(kind)+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("%s differs from %s; run go test ./internal/notice -update and review the diff", kind, path)
			}
		})
	}
}

func TestRender_EscapesWhatPeopleTyped(t *testing.T) {
	d := sampleData()
	d.Title = `<script>alert(1)</script> & "charcoal"`
	d.Reason = `<img src=x onerror=alert(1)>`
	e, err := notice.Render(notice.Cancelled, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.HTML, "<script>") || strings.Contains(e.HTML, "<img") {
		t.Errorf("html carries raw markup:\n%s", e.HTML)
	}
	if !strings.Contains(e.Text, `<script>alert(1)</script> & "charcoal"`) {
		t.Errorf("text should carry the words as typed:\n%s", e.Text)
	}
	if strings.ContainsAny(e.Subject, "\r\n") {
		t.Errorf("subject has a line break: %q", e.Subject)
	}
}

func TestRender_KeepsSubjectsOnOneLine(t *testing.T) {
	d := sampleData()
	d.Title = "Charcoal\r\nBcc: someone@example.test"
	e, err := notice.Render(notice.Confirmed, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(e.Subject, "\r\n") {
		t.Errorf("subject = %q", e.Subject)
	}
}

func TestRender_OnlyPrivateKindsCarryAPrivateLink(t *testing.T) {
	for _, kind := range notice.Kinds {
		e, err := notice.Render(kind, sampleData())
		if err != nil {
			t.Fatal(err)
		}
		carries := strings.Contains(e.Text+e.HTML, "PRIVATE")
		if carries != (kind == notice.Joined || kind == notice.OwnerCreated) {
			t.Errorf("%s carries the private link: %v", kind, carries)
		}
	}
}

func TestRender_RefusesAnUnknownKind(t *testing.T) {
	if _, err := notice.Render("party", sampleData()); err == nil {
		t.Error("an unknown kind rendered")
	}
}

func TestInviteMessage_SaysWhatItTakes(t *testing.T) {
	got := notice.InviteMessage("Life drawing for beginners", "The Long Room", "$45", 3, "https://example.test/w/abc")
	want := "I’m planning Life drawing for beginners at The Long Room. It’s $45 a spot, and it only happens if 3 people reserve; otherwise nobody pays. Tell me when you can come: https://example.test/w/abc"
	if got != want {
		t.Errorf("message = %q", got)
	}
	if got := notice.InviteMessage("Charcoal", "", "$10", 1, "x"); !strings.Contains(got, "I’m planning Charcoal. It’s $10 a spot, and it only happens if 1 person reserves;") {
		t.Errorf("message = %q", got)
	}
}

func TestRender_CancellationsSayWhatHappened(t *testing.T) {
	d := sampleData()
	d.Opened, d.Closed = true, true
	e, _ := notice.Render(notice.OwnerCancelled, d)
	if !strings.Contains(e.Text, "Not enough people reserved by Fri, Nov 13 at 10 AM (New York time), so Life drawing for beginners won’t happen.") {
		t.Errorf("closed at the deadline:\n%s", e.Text)
	}
	d.Opened, d.Closed, d.Reason = false, false, "I’m ill."
	e, _ = notice.Render(notice.OwnerCancelled, d)
	if !strings.Contains(e.Text, "You said: “I’m ill.”") || !strings.Contains(e.Text, "Nobody had reserved, so nothing was held or charged.") || strings.Contains(e.Text, "refunded") {
		t.Errorf("cancelled before reservations opened:\n%s", e.Text)
	}
	p, _ := notice.Render(notice.Cancelled, d)
	if !strings.Contains(p.Text, "The organizer said: “I’m ill.”") || strings.Contains(p.Text, "refund") {
		t.Errorf("participant:\n%s", p.Text)
	}
}

func TestRender_OnePersonPaid(t *testing.T) {
	d := sampleData()
	d.Paid = 1
	e, _ := notice.Render(notice.OwnerConfirmed, d)
	if !strings.Contains(e.Text, "1 person paid") || strings.Contains(e.Text, "1 people") {
		t.Errorf("text:\n%s", e.Text)
	}
}
