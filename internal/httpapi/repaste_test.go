package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/demo"
)

type chatRepasteJSON struct {
	Imported []chatPersonLinkJSON `json:"imported"`
	Updated  []chatPersonLinkJSON `json:"updated"`
	Skipped  []chatSkippedJSON    `json:"skipped"`
}

type chatPersonLinkJSON struct {
	Name           string `json:"name"`
	ParticipantURL string `json:"participant_url"`
}

type chatPreviewPersonJSON struct {
	Name        string `json:"name"`
	Excerpt     string `json:"excerpt"`
	Messages    int    `json:"messages"`
	NewMessages int    `json:"new_messages"`
	Host        bool   `json:"host"`
	Joined      bool   `json:"joined"`
}

type ownerUpdateJSON struct {
	DemoUpdate *string `json:"demo_update"`
}

var graceReply = demo.LifeDrawing.Replies[0]

func withGracesReply(chat string) string {
	return chat + "\n" + graceReply.Name + ": " + graceReply.Message
}

func (e *env) repaste(h workshopHandle, text string, names ...string) chatRepasteJSON {
	e.t.Helper()
	var out chatRepasteJSON
	e.call(http.MethodPost, h.ownerPath+"/chat/import", importBody(text, names...), http.StatusAccepted, &out)
	return out
}

func (e *env) previewPeople(h workshopHandle, text string) map[string]chatPreviewPersonJSON {
	e.t.Helper()
	var out struct {
		People []chatPreviewPersonJSON `json:"people"`
	}
	e.call(http.MethodPost, h.ownerPath+"/chat/preview", map[string]string{"text": text}, http.StatusOK, &out)
	people := make(map[string]chatPreviewPersonJSON, len(out.People))
	for _, p := range out.People {
		people[p.Name] = p
	}
	return people
}

func (e *env) demoUpdate(h workshopHandle) *string {
	e.t.Helper()
	var v ownerUpdateJSON
	e.call(http.MethodGet, h.ownerPath, nil, http.StatusOK, &v)
	return v.DemoUpdate
}

func (e *env) answersOf(participantID string) int {
	e.t.Helper()
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, `select count(*) from availabilities where participant_id = $1`, participantID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func canCome(v ownerViewJSON, dateID string) []string {
	for _, d := range v.Recommendation.PerDate {
		if d.DateID == dateID {
			return d.Compatible
		}
	}
	return nil
}

func alreadyHere(names ...string) []chatSkippedJSON {
	out := make([]chatSkippedJSON, len(names))
	for i, n := range names {
		out[i] = chatSkippedJSON{Name: n, Reason: "already_here"}
	}
	return out
}

func (r *recordingAI) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.reads))
	for i, in := range r.reads {
		out[i] = in.Message
	}
	return out
}

func TestImport_RepasteReadsOnlyNewMessages(t *testing.T) {
	model := &recordingAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)
	e.background.Wait()
	before := e.owner(h)
	if n := len(canCome(before, h.satID)); n != 2 {
		t.Fatalf("Saturday before Grace's reply has %d who can come, want 2", n)
	}
	grace := participantNamed(t, before, "Grace Liu")
	read := len(model.messages())

	later := withGracesReply(lifeDrawingChat())
	people := e.previewPeople(h, later)
	if g := people["Grace Liu"]; !g.Joined || g.NewMessages != 1 || g.Messages != 2 || g.Excerpt != graceReply.Message {
		t.Errorf("Grace in the preview = %+v, want here with one new message, shown as the excerpt", g)
	}
	if p := people["Priya Raman"]; !p.Joined || p.NewMessages != 0 || p.Excerpt != demo.LifeDrawing.People[0].Message {
		t.Errorf("Priya in the preview = %+v, want already here with nothing new", p)
	}

	got := e.repaste(h, later, castNames()...)
	if len(got.Imported) != 0 || len(got.Updated) != 1 || got.Updated[0].Name != "Grace Liu" || got.Updated[0].ParticipantURL != *grace.ParticipantURL {
		t.Fatalf("re-paste = %+v, want Grace updated through her own link and nobody added", got)
	}
	if want := alreadyHere("Priya Raman", "Ben Whitaker", "Lucía Torres", "Hannah Kim", "Tomás Silva"); !slices.Equal(got.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", got.Skipped, want)
	}
	e.background.Wait()

	if sent := model.messages()[read:]; !slices.Equal(sent, []string{graceReply.Message}) {
		t.Errorf("Claude read %q after the re-paste, want only Grace's reply", sent)
	}
	earlier := model.earlier()[len(model.earlier())-1]
	if earlier == nil || !slices.Equal(earlier.Messages, []string{demo.LifeDrawing.People[5].Message}) || len(earlier.Questions) != 1 || !strings.Contains(earlier.Questions[0], "10 AM–12 PM") {
		t.Errorf("Grace's reply was read with %+v, want her first message and Claude's question", earlier)
	}
	v := e.owner(h)
	if sat := canCome(v, h.satID); len(sat) != 3 || !slices.Contains(sat, grace.ID) {
		t.Errorf("Saturday after Grace's reply = %v, want 3 with Grace", sat)
	}
	if !v.Recommendation.MeetsMinimum || v.Recommendation.DateID != h.satID {
		t.Errorf("recommendation = %+v, want Saturday with enough people", v.Recommendation)
	}
	if g := participantNamed(t, v, "Grace Liu"); g.ChatMessage == nil || *g.ChatMessage != demo.LifeDrawing.People[5].Message+"\n"+graceReply.Message {
		t.Errorf("Grace's chat message = %v, want both of her messages", g.ChatMessage)
	}
	for _, p := range v.Participants {
		want := 1
		if p.ID == grace.ID {
			want = 2
		}
		if n := e.answersOf(p.ID); n != want {
			t.Errorf("%s has %d readings, want %d", p.DisplayName, n, want)
		}
	}
	if v.Reading != (readingJSON{Pending: 0, Total: 6}) {
		t.Errorf("reading after the re-paste = %+v", v.Reading)
	}
}

func TestImport_RepasteWithNothingNew(t *testing.T) {
	model := &recordingAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)
	e.background.Wait()
	read := len(model.messages())

	later := withGracesReply(lifeDrawingChat())
	e.repaste(h, later, castNames()...)
	e.background.Wait()
	read++

	for _, text := range []string{lifeDrawingChat(), later} {
		for name, p := range e.previewPeople(h, text) {
			if name != "Iris" && (!p.Joined || p.NewMessages != 0) {
				t.Errorf("%s in the preview = %+v, want already here with nothing new", name, p)
			}
		}
		got := e.repaste(h, text, castNames()...)
		if len(got.Imported)+len(got.Updated) != 0 || !slices.Equal(got.Skipped, alreadyHere(castNames()...)) {
			t.Errorf("a re-paste with nothing new = %+v, want everyone already here", got)
		}
	}
	e.background.Wait()
	if n := len(model.messages()); n != read {
		t.Errorf("Claude read %d messages, want %d: nothing new was pasted", n, read)
	}
	e.repaste(h, chatOf("Ana"), "Ana")
	e.wantError(http.MethodPost, h.ownerPath+"/chat/import", importBody(chatOf("Ben"), "Ben"), http.StatusTooManyRequests, "import_limit")
}

func TestImport_RepasteEditedChatIsIgnored(t *testing.T) {
	model := &recordingAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)
	e.background.Wait()
	read := len(model.messages())
	priya := demo.LifeDrawing.People[0]

	edited := strings.Replace(lifeDrawingChat(), priya.Message, "Saturday works for me, Sunday maybe.", 1) + "\nPriya Raman: Actually Sunday works too."
	if p := e.previewPeople(h, edited)["Priya Raman"]; !p.Joined || p.NewMessages != 0 {
		t.Errorf("Priya in an edited chat = %+v, want already here with nothing new", p)
	}
	got := e.repaste(h, edited, "Priya Raman")
	if len(got.Updated) != 0 || !slices.Equal(got.Skipped, alreadyHere("Priya Raman")) {
		t.Errorf("an edited chat = %+v, want Priya already here", got)
	}

	other := "[11/12/26, 9:01:00 AM] Priya Raman: Sunday works now.\n[11/12/26, 9:02:00 AM] Grace Liu: " + graceReply.Message
	got = e.repaste(h, other, "Priya Raman", "Grace Liu")
	if len(got.Updated) != 0 || !slices.Equal(got.Skipped, alreadyHere("Priya Raman", "Grace Liu")) {
		t.Errorf("another export without the earlier messages = %+v, want both already here", got)
	}
	e.background.Wait()
	if n := len(model.messages()); n != read {
		t.Errorf("Claude read %d more messages from edited chats, want none", n-read)
	}
	if p := participantNamed(t, e.owner(h), "Priya Raman"); *p.ChatMessage != priya.Message {
		t.Errorf("Priya's chat message = %q, want it unchanged", *p.ChatMessage)
	}
}

func TestImport_RepasteReadsSomeoneUnreadWhole(t *testing.T) {
	model := &marketOutageAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	model.failing.Store(true)
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)
	e.background.Wait()
	if g := participantNamed(t, e.owner(h), "Grace Liu"); !g.Unread {
		t.Fatalf("Grace = %+v, want unread", g)
	}
	model.failing.Store(false)

	got := e.repaste(h, lifeDrawingChat()+"\nGrace Liu: Saturday works now.", "Grace Liu")
	if len(got.Updated) != 1 {
		t.Fatalf("re-paste = %+v, want Grace updated", got)
	}
	e.background.Wait()
	v := e.owner(h)
	grace := participantNamed(t, v, "Grace Liu")
	for _, a := range v.Availabilities {
		if a.ParticipantID == grace.ID && a.Message != demo.LifeDrawing.People[5].Message+"\nSaturday works now." {
			t.Errorf("Grace was read from %q, want both of her messages: Claude never read the first", a.Message)
		}
	}
	if grace.Unread || e.answersOf(grace.ID) != 1 {
		t.Errorf("Grace = %+v with %d readings, want read once", grace, e.answersOf(grace.ID))
	}
}

func TestImport_RepasteKeepsTheNewestWhenLong(t *testing.T) {
	model := &recordingAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop()
	long := make([]string, 30)
	for i := range long {
		long[i] = "Ana: " + strings.Repeat("Saturday or Sunday, either works for me. ", 2) + strings.Repeat("x", i)
	}
	first := strings.Join(long, "\n")
	e.importChat(h, first, "Ana")
	e.background.Wait()
	ana := participantNamed(t, e.owner(h), "Ana")
	if n := len([]rune(*ana.ChatMessage)); n > 2000 || n < 1900 {
		t.Fatalf("Ana's stored text has %d runes, want the newest messages up to 2000", n)
	}

	got := e.repaste(h, first+"\nAna: Only Sunday now.", "Ana")
	if len(got.Updated) != 1 {
		t.Fatalf("re-paste after a long first paste = %+v, want Ana updated", got)
	}
	e.background.Wait()
	if sent := model.messages(); sent[len(sent)-1] != "Only Sunday now." {
		t.Errorf("Claude read %q, want only Ana's new message", sent[len(sent)-1])
	}
	stored := *participantNamed(t, e.owner(h), "Ana").ChatMessage
	if n := len([]rune(stored)); n > 2000 || !strings.HasSuffix(stored, "\nOnly Sunday now.") {
		t.Errorf("Ana's stored text has %d runes and ends %q, want at most 2000 ending with the new message", n, stored[len(stored)-20:])
	}
}

func TestImport_RepasteWaitsForAReadingInFlight(t *testing.T) {
	model := &heldAI{Fake: ai.Fake{Script: demo.FakeScript()}, release: make(chan struct{})}
	t.Cleanup(model.open)
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)

	later := withGracesReply(lifeDrawingChat())
	got := e.repaste(h, later, "Grace Liu")
	if len(got.Updated) != 0 || !slices.Equal(got.Skipped, []chatSkippedJSON{{Name: "Grace Liu", Reason: "still_reading"}}) {
		t.Errorf("re-paste while Claude reads Grace = %+v, want her skipped as still being read", got)
	}
	model.open()
	e.background.Wait()
	if g := participantNamed(t, e.owner(h), "Grace Liu"); *g.ChatMessage != demo.LifeDrawing.People[5].Message {
		t.Errorf("Grace's chat message = %q, want her first message only, so the next paste reads the reply", *g.ChatMessage)
	}
	if got := e.repaste(h, later, "Grace Liu"); len(got.Updated) != 1 {
		t.Errorf("re-paste once Claude has read her = %+v, want Grace updated", got)
	}
	e.background.Wait()
}

type roomAI struct {
	ai.Fake
	room atomic.Int32
}

func (r *roomAI) Room(calls int) time.Duration { return r.RoomLeaving(calls, nil) }

func (r *roomAI) RoomLeaving(calls int, _ func(int) int) time.Duration {
	if calls > int(r.room.Load()) {
		return time.Minute
	}
	return 0
}

func TestImport_RepasteNeedsRoomForTheUpdates(t *testing.T) {
	model := &roomAI{Fake: ai.Fake{Script: demo.FakeScript()}}
	model.room.Store(6)
	e := newEnv(t, withAI(model))
	h := e.createWorkshop(withHost("Iris Calder"))
	e.importChat(h, lifeDrawingChat(), castNames()...)
	e.background.Wait()

	model.room.Store(1)
	twoNew := withGracesReply(lifeDrawingChat()) + "\nPriya Raman: Sunday is fine too now."
	e.wantError(http.MethodPost, h.ownerPath+"/chat/import", importBody(twoNew, castNames()...), http.StatusTooManyRequests, "ai_capped")
	v := e.owner(h)
	if g := participantNamed(t, v, "Grace Liu"); *g.ChatMessage != demo.LifeDrawing.People[5].Message || e.answersOf(g.ID) != 1 {
		t.Errorf("Grace after a refused re-paste = %+v, want her first message, read once", g)
	}
	if got := e.repaste(h, twoNew, "Grace Liu"); len(got.Updated) != 1 {
		t.Errorf("one update with room for one = %+v", got)
	}
	e.background.Wait()
}

func TestImport_DemoReadsOneUpdatedChat(t *testing.T) {
	e := newEnv(t, withDemoMode())
	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	h := demoHandle(d)
	if u := e.demoUpdate(h); u != nil {
		t.Errorf("a new demo offers the updated chat %q before its first import", *u)
	}
	e.importDemoChat(h)
	e.background.Wait()

	offer := e.demoUpdate(h)
	if offer == nil {
		t.Fatal("after the import, the demo does not offer Iris's chat with Grace's reply")
	}
	chat := *e.owner(h).DemoChat
	if !strings.HasPrefix(*offer, chat+"\n[") || !strings.HasSuffix(*offer, " PM] Grace Liu: "+graceReply.Message) {
		t.Errorf("offered chat =\n%s\nwant Iris's chat with Grace's reply appended", *offer)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/chat/import", importBody(*offer+"\n[11/11/26, 8:10:00\u202fAM] New Person: Saturday works.", append(castNames(), "New Person")...), http.StatusUnprocessableEntity, "demo_import_used")

	got := e.repaste(h, *offer, castNames()...)
	if len(got.Updated) != 1 || got.Updated[0].Name != "Grace Liu" || len(got.Imported) != 0 {
		t.Fatalf("demo re-paste = %+v, want Grace updated", got)
	}
	e.background.Wait()
	v := e.owner(h)
	if sat := canCome(v, v.Dates[0].ID); len(sat) != 3 || !v.Recommendation.MeetsMinimum {
		t.Errorf("Saturday after Grace's reply in the demo = %v, recommendation %+v; want 3 and enough", sat, v.Recommendation)
	}
	if !v.ChatImported || e.demoUpdate(h) != nil {
		t.Error("the demo still offers the updated chat after reading it")
	}
	var imports int
	grace := participantNamed(t, v, "Grace Liu")
	if err := e.st.Pool.QueryRow(e.ctx, `select w.chat_imports from workshops w join participants p on p.workshop_id = w.id where p.id = $1`, grace.ID).Scan(&imports); err != nil || imports != 2 {
		t.Errorf("chat_imports = %d, %v; want 2", imports, err)
	}

	again := *offer + "\n[11/11/26, 8:15:00 AM] Grace Liu: Saturday still works."
	e.wantError(http.MethodPost, h.ownerPath+"/chat/import", importBody(again, castNames()...), http.StatusUnprocessableEntity, "demo_update_used")
	if same := e.repaste(h, *offer, castNames()...); len(same.Updated) != 0 {
		t.Errorf("pasting the same updated chat again = %+v, want nothing to read", same)
	}
}

func TestImport_DemoOffersTheUpdateOnlyWhileGraceIsUnclear(t *testing.T) {
	model := &heldAI{Fake: ai.Fake{Script: demo.FakeScript()}, release: make(chan struct{})}
	t.Cleanup(model.open)
	e := newEnv(t, withDemoMode(), withAI(model))
	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	h := demoHandle(d)
	imported := e.importDemoChat(h)
	if u := e.demoUpdate(h); u != nil {
		t.Error("the demo offers the updated chat while Claude is still reading Grace's first message")
	}
	model.open()
	e.background.Wait()
	if e.demoUpdate(h) == nil {
		t.Fatal("no updated chat once Grace's question is open")
	}

	grace := imported.Imported[5]
	e.call(http.MethodPost, "/api/v1/p/"+participantToken(grace.ParticipantURL)+"/availability", map[string]string{"message": graceReply.Message}, http.StatusCreated, nil)
	if u := e.demoUpdate(h); u != nil {
		t.Error("the demo still offers the updated chat after Grace answered on her page")
	}
	if sat := canCome(e.owner(h), e.owner(h).Dates[0].ID); len(sat) != 3 {
		t.Errorf("Saturday after Grace answered on her page = %v, want 3", sat)
	}
}
