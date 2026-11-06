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

func maya() notice.Data {
	return notice.Data{
		Name:          "Priya Raman",
		Title:         "Wheel-throwing for beginners",
		Host:          "Maya Okafor",
		Place:         "Maya's studio · Greenpoint, Brooklyn",
		When:          "Sat, Nov 14, 10 AM–12 PM (New York time)",
		Price:         "$45",
		Needed:        3,
		Paid:          4,
		Collected:     "$180",
		Deadline:      "Fri, Nov 13 at 10 AM (New York time)",
		Link:          "https://example.test/p/PRIVATE",
		InviteLink:    "https://example.test/w/INVITE",
		InviteMessage: "I’m planning Wheel-throwing for beginners. Tell me when you can come: https://example.test/w/INVITE",
	}
}

func TestRender_EveryKindMatchesItsGolden(t *testing.T) {
	for _, kind := range notice.Kinds {
		t.Run(string(kind), func(t *testing.T) {
			d := maya()
			if kind == notice.Cancelled || kind == notice.OwnerCancelled {
				d.Reason = "The studio flooded."
			}
			if kind == notice.OwnerCreated {
				d.Name = "Maya Okafor"
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
	d := maya()
	d.Title = `<script>alert(1)</script> & "clay"`
	d.Reason = `<img src=x onerror=alert(1)>`
	e, err := notice.Render(notice.Cancelled, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.HTML, "<script>") || strings.Contains(e.HTML, "<img") {
		t.Errorf("html carries raw markup:\n%s", e.HTML)
	}
	if !strings.Contains(e.Text, `<script>alert(1)</script> & "clay"`) {
		t.Errorf("text should carry the words as typed:\n%s", e.Text)
	}
	if strings.ContainsAny(e.Subject, "\r\n") {
		t.Errorf("subject has a line break: %q", e.Subject)
	}
}

func TestRender_KeepsSubjectsOnOneLine(t *testing.T) {
	d := maya()
	d.Title = "Clay\r\nBcc: someone@example.test"
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
		e, err := notice.Render(kind, maya())
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
	if _, err := notice.Render("party", maya()); err == nil {
		t.Error("an unknown kind rendered")
	}
}

func TestInviteMessage_SaysWhatItTakes(t *testing.T) {
	got := notice.InviteMessage("Wheel-throwing for beginners", "Maya's studio", "$45", 3, "https://example.test/w/abc")
	want := "I’m planning Wheel-throwing for beginners at Maya's studio. It’s $45 a spot, and it only happens if 3 people reserve; otherwise nobody pays. Tell me when you can come: https://example.test/w/abc"
	if got != want {
		t.Errorf("message = %q", got)
	}
	if got := notice.InviteMessage("Clay", "", "$10", 1, "x"); !strings.Contains(got, "I’m planning Clay. It’s $10 a spot, and it only happens if 1 person reserves;") {
		t.Errorf("message = %q", got)
	}
}
