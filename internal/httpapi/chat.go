package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/chatimport"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	maxChatBodyBytes   = 256 << 10
	chatExcerptRunes   = 140
	chatImportsAllowed = 3
	chatImportWindow   = 10 * time.Minute
	demoChatPeople     = 8
	demoChatReads      = 2
	chatReaders        = 2
	chatReadTimeout    = 5 * time.Minute
	chatEditTimeout    = 30 * time.Second
)

const (
	skippedHost        = "host"
	skippedAlreadyHere = "already_here"
	skippedNotInChat   = "not_in_chat"
	skippedReading     = "still_reading"
)

var (
	errChatTooLong = newError(http.StatusRequestEntityTooLarge, "chat_too_long",
		"The chat is longer than 20,000 characters. Paste only the part where people answered.")
	errChatEmpty   = newError(http.StatusUnprocessableEntity, "chat_empty", "Paste the group chat first.")
	errChatNoLines = newError(http.StatusUnprocessableEntity, "chat_no_lines",
		"No lines like “Name: message” were found. Paste the messages as text, or export the chat without media.")
	errChatTooManyPeople = newError(http.StatusUnprocessableEntity, "chat_too_many_people",
		"The chat has more than 30 people. Paste only the part where the people for this class answered.")
	errNotCollecting = conflict("not_collecting",
		"People can be added from the chat only while you are still finding the date.")
	errDemoImportUsed = newError(http.StatusUnprocessableEntity, "demo_import_used",
		"This demo class already has its people. Start a new demo to paste another chat.")
	errDemoTooManyPeople = newError(http.StatusUnprocessableEntity, "demo_too_many_people",
		"A demo class takes at most 8 people from the chat.")
	errDemoUpdateUsed = newError(http.StatusUnprocessableEntity, "demo_update_used",
		"This demo already read new messages from the chat once. Start a new demo to try it again.")
	errCannotRemove = conflict("cannot_remove",
		"Only someone added from the chat who has not reserved can be removed, and only while you are still finding the date.")
	errStillReading = conflict("cannot_remove", "Claude is still reading this person's message. Try again in a moment.")
)

type chatImports struct {
	slots     chan struct{}
	mu        sync.Mutex
	recent    map[string]*slidingCap
	lastSweep time.Time
	reading   map[string]string
	busy      map[string]int
}

func newChatImports() *chatImports {
	return &chatImports{
		slots:   make(chan struct{}, chatReaders),
		recent:  map[string]*slidingCap{},
		reading: map[string]string{},
		busy:    map[string]int{},
	}
}

func (c *chatImports) take(workshopID string, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastSweep) > sweepEvery {
		for id, window := range c.recent {
			window.wait(now)
			if len(window.times) == 0 {
				delete(c.recent, id)
			}
		}
		c.lastSweep = now
	}
	window, ok := c.recent[workshopID]
	if !ok {
		window = &slidingCap{limit: chatImportsAllowed, within: chatImportWindow}
		c.recent[workshopID] = window
	}
	if wait := window.wait(now); wait > 0 {
		return wait
	}
	window.add(now)
	return 0
}

func (c *chatImports) refund(workshopID string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if window, ok := c.recent[workshopID]; ok {
		window.remove(at)
	}
}

func (c *chatImports) queued() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reading)
}

func (c *chatImports) startReading(workshopID string, people []chatReading) []chatReading {
	c.mu.Lock()
	defer c.mu.Unlock()
	started := make([]chatReading, 0, len(people))
	for _, p := range people {
		if _, ok := c.reading[p.id]; ok {
			continue
		}
		c.reading[p.id] = workshopID
		c.busy[workshopID]++
		started = append(started, p)
	}
	return started
}

func (c *chatImports) doneReading(workshopID, participantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reading, participantID)
	if c.busy[workshopID]--; c.busy[workshopID] <= 0 {
		delete(c.busy, workshopID)
	}
}

func (c *chatImports) isReading(participantID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.reading[participantID]
	return ok
}

func (c *chatImports) active(workshopID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy[workshopID] > 0
}

type chatReading struct {
	id   string
	p    demo.Participant
	undo *chatUndo
}

type chatUndo struct {
	wrote, previous string
}

type chatPersonDTO struct {
	Name        string `json:"name"`
	Excerpt     string `json:"excerpt"`
	Messages    int    `json:"messages"`
	NewMessages int    `json:"new_messages"`
	Host        bool   `json:"host"`
	Joined      bool   `json:"joined"`
}

type chatPreviewDTO struct {
	People  []chatPersonDTO `json:"people"`
	Skipped int             `json:"skipped"`
}

type chatImportedDTO struct {
	Name           string `json:"name"`
	ParticipantURL string `json:"participant_url"`
}

type chatSkippedDTO struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type chatImportDTO struct {
	Imported []chatImportedDTO `json:"imported"`
	Updated  []chatImportedDTO `json:"updated"`
	Skipped  []chatSkippedDTO  `json:"skipped"`
}

func chatTextPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/owner/") &&
		(strings.HasSuffix(path, "/chat/preview") || strings.HasSuffix(path, "/chat/import"))
}

func decodeChat(r *http.Request, dst any) error {
	err := decode(r, dst)
	if errors.Is(err, errBodyTooLarge) {
		return errChatTooLong
	}
	return err
}

func parseChat(text string) (chatimport.Chat, error) {
	chat, err := chatimport.Parse(text)
	switch {
	case errors.Is(err, chatimport.ErrTooLong):
		return chat, errChatTooLong
	case errors.Is(err, chatimport.ErrEmpty):
		return chat, errChatEmpty
	case errors.Is(err, chatimport.ErrTooManyPeople):
		return chat, errChatTooManyPeople
	case errors.Is(err, chatimport.ErrNoLines):
		return chat, errChatNoLines
	}
	return chat, err
}

func isHost(name, host string) bool {
	words := strings.Fields(host)
	if len(words) == 0 {
		return false
	}
	return chatimport.SameName(name, host) || chatimport.StartsWithName(name, words[0])
}

func sameNameAs(name string) func(string) bool {
	return func(other string) bool { return chatimport.SameName(name, other) }
}

func joinedAs(name string, participants []store.ParticipantRow) bool {
	return slices.ContainsFunc(participants, func(p store.ParticipantRow) bool { return chatimport.SameName(name, p.DisplayName) })
}

func excerpt(message string) string {
	text := strings.Join(strings.Fields(message), " ")
	if r := []rune(text); len(r) > chatExcerptRunes {
		return strings.TrimSpace(string(r[:chatExcerptRunes])) + "…"
	}
	return text
}

type chatUpdate struct {
	participant store.ParticipantRow
	fresh       []string
	stored      string
}

func chatParticipant(name string, participants []store.ParticipantRow) (store.ParticipantRow, bool) {
	i := slices.IndexFunc(participants, func(p store.ParticipantRow) bool {
		return p.Source == store.SourceChat && p.ChatMessage != nil && p.ChatLinkToken != nil && chatimport.SameName(name, p.DisplayName)
	})
	if i < 0 {
		return store.ParticipantRow{}, false
	}
	return participants[i], true
}

func messagesAfter(p chatimport.Person, stored string) []string {
	for i := len(p.Messages); i > 0; i-- {
		if (chatimport.Person{Messages: p.Messages[:i]}).Text(chatimport.MaxTextRunes) == stored {
			return p.Messages[i:]
		}
	}
	return nil
}

func updateFor(p chatimport.Person, participants []store.ParticipantRow) (chatUpdate, bool) {
	row, ok := chatParticipant(p.Name, participants)
	if !ok {
		return chatUpdate{}, false
	}
	fresh := messagesAfter(p, *row.ChatMessage)
	if len(fresh) == 0 {
		return chatUpdate{}, false
	}
	return chatUpdate{participant: row, fresh: fresh, stored: p.Text(chatimport.MaxTextRunes)}, true
}

type chatFollowUp struct {
	message string
	undo    *chatUndo
}

func (u chatUpdate) followUp(answered bool) chatFollowUp {
	if !answered {
		return chatFollowUp{message: u.stored}
	}
	return chatFollowUp{
		message: chatimport.Person{Messages: u.fresh}.Text(chatimport.MaxTextRunes),
		undo:    &chatUndo{wrote: u.stored, previous: *u.participant.ChatMessage},
	}
}

func (s *server) previewChat(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeChat(r, &req); err != nil {
		return err
	}
	chat, err := parseChat(req.Text)
	if err != nil {
		return err
	}
	existing, err := s.Store.ParticipantsByWorkshop(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	out := chatPreviewDTO{People: make([]chatPersonDTO, len(chat.People)), Skipped: chat.Skipped}
	for i, p := range chat.People {
		person := chatPersonDTO{
			Name:     p.Name,
			Excerpt:  excerpt(p.Messages[0]),
			Messages: len(p.Messages),
			Host:     isHost(p.Name, ws.HostName),
			Joined:   joinedAs(p.Name, existing),
		}
		if u, ok := updateFor(p, existing); ok && person.Joined && !person.Host {
			person.NewMessages = len(u.fresh)
			person.Excerpt = excerpt(u.fresh[len(u.fresh)-1])
		}
		out.People[i] = person
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func validNames(names []string) error {
	if len(names) == 0 || len(names) > chatimport.MaxPeople {
		return badRequest("invalid_names", "Choose between 1 and %d people to add.", chatimport.MaxPeople)
	}
	for _, n := range names {
		if l := utf8.RuneCountInString(strings.TrimSpace(n)); l == 0 || l > maxDisplayName {
			return badRequest("invalid_names", "Each name must have between 1 and %d characters.", maxDisplayName)
		}
	}
	return nil
}

type importPlan struct {
	people  []chatimport.Person
	updates []chatUpdate
	skipped []chatSkippedDTO
}

func (p importPlan) readings() int {
	return len(p.people) + len(p.updates)
}

func planImport(chat chatimport.Chat, names []string, host string, existing []store.ParticipantRow, reading func(string) bool) importPlan {
	plan := importPlan{skipped: []chatSkippedDTO{}}
	for _, p := range chat.People {
		if !slices.ContainsFunc(names, sameNameAs(p.Name)) {
			continue
		}
		if isHost(p.Name, host) {
			plan.skipped = append(plan.skipped, chatSkippedDTO{Name: p.Name, Reason: skippedHost})
			continue
		}
		if !joinedAs(p.Name, existing) {
			plan.people = append(plan.people, p)
			continue
		}
		u, ok := updateFor(p, existing)
		switch {
		case !ok:
			plan.skipped = append(plan.skipped, chatSkippedDTO{Name: p.Name, Reason: skippedAlreadyHere})
		case reading(u.participant.ID):
			plan.skipped = append(plan.skipped, chatSkippedDTO{Name: p.Name, Reason: skippedReading})
		default:
			plan.updates = append(plan.updates, u)
		}
	}
	var missing []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		inChat := slices.ContainsFunc(chat.People, func(p chatimport.Person) bool { return chatimport.SameName(n, p.Name) })
		if inChat || slices.ContainsFunc(missing, sameNameAs(n)) {
			continue
		}
		missing = append(missing, n)
		plan.skipped = append(plan.skipped, chatSkippedDTO{Name: n, Reason: skippedNotInChat})
	}
	return plan
}

func demoAllows(plan importPlan, st store.ChatImportState) error {
	switch {
	case len(plan.people) > 0 && (st.Imports > 0 || len(st.Participants) > 0):
		return errDemoImportUsed
	case len(plan.people) > demoChatPeople:
		return errDemoTooManyPeople
	case len(plan.updates) > 0 && st.Imports >= demoChatReads:
		return errDemoUpdateUsed
	}
	return nil
}

func (s *server) roomToRead(w http.ResponseWriter, ws store.WorkshopRow, readings int) error {
	c, ok := s.AI.(ai.Capacity)
	if !ok {
		return nil
	}
	queued := s.chats.queued()
	wait := c.RoomLeaving(readings+queued, windowReserve)
	if wait <= 0 {
		return nil
	}
	loc, err := time.LoadLocation(ws.Timezone)
	if err != nil {
		return err
	}
	s.Log.Warn("chat import: no room to read everyone", "workshop_id", ws.ID, "readings", readings, "queued", queued, "retry_after", wait)
	return modelCapped(w, wait, s.Clock.Now(), loc)
}

func (s *server) countRead(w http.ResponseWriter, workshopID string, now time.Time) error {
	wait := s.chats.take(workshopID, now)
	if wait <= 0 {
		return nil
	}
	retryAfter(w, wait)
	return newError(http.StatusTooManyRequests, "import_limit",
		"Claude has read this class's chat three times in the last 10 minutes. Try again in "+minutes(wait)+".")
}

func (s *server) importChat(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		Text  string   `json:"text"`
		Names []string `json:"names"`
	}
	if err := decodeChat(r, &req); err != nil {
		return err
	}
	if err := validNames(req.Names); err != nil {
		return err
	}
	chat, err := parseChat(req.Text)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), chatEditTimeout)
	defer cancel()
	now := s.Clock.Now()
	counted := false
	var plan importPlan
	followUps := map[string]chatFollowUp{}
	res, err := s.Store.ImportChatParticipants(ctx, ws.ID, func(st store.ChatImportState) (store.ChatPlan, error) {
		cur := st.Workshop
		if cur.Status != domain.WorkshopCollecting {
			return store.ChatPlan{}, errNotCollecting
		}
		plan = planImport(chat, req.Names, cur.HostName, st.Participants, s.chats.isReading)
		if cur.IsDemo {
			if err := demoAllows(plan, st); err != nil {
				return store.ChatPlan{}, err
			}
		}
		if plan.readings() == 0 {
			return store.ChatPlan{}, nil
		}
		if len(st.Participants)+len(plan.people) > maxParticipantsPerWorkshop {
			return store.ChatPlan{}, conflict("too_many_participants", "This workshop already has as many participants as it can take.")
		}
		if err := s.roomToRead(w, cur, plan.readings()); err != nil {
			return store.ChatPlan{}, err
		}
		if err := s.countRead(w, cur.ID, now); err != nil {
			return store.ChatPlan{}, err
		}
		counted = true
		todo := store.ChatPlan{}
		for _, p := range plan.people {
			todo.Add = append(todo.Add, store.ChatPerson{DisplayName: p.Name, Message: p.Text(chatimport.MaxTextRunes)})
		}
		for _, u := range plan.updates {
			todo.Update = append(todo.Update, store.ChatUpdate{ParticipantID: u.participant.ID, Message: u.stored})
			followUps[u.participant.ID] = u.followUp(st.Answered[u.participant.ID])
		}
		return todo, nil
	})
	if err != nil {
		if counted {
			s.chats.refund(ws.ID, now)
		}
		return err
	}
	out := chatImportDTO{
		Imported: make([]chatImportedDTO, 0, len(res.Imported)),
		Updated:  make([]chatImportedDTO, 0, len(res.Updated)),
		Skipped:  plan.skipped,
	}
	readings := make([]chatReading, 0, len(res.Imported)+len(res.Updated))
	for _, p := range res.Imported {
		link := s.Cfg.BaseURL + "/p/" + p.Token
		message := ""
		if p.ChatMessage != nil {
			message = *p.ChatMessage
		}
		readings = append(readings, chatReading{id: p.ID, p: demo.Participant{Name: p.DisplayName, Message: message, URL: link, Token: p.Token}})
		out.Imported = append(out.Imported, chatImportedDTO{Name: p.DisplayName, ParticipantURL: link})
	}
	for _, p := range res.Updated {
		link := s.Cfg.BaseURL + "/p/" + p.Token
		f := followUps[p.ID]
		readings = append(readings, chatReading{id: p.ID, p: demo.Participant{Name: p.DisplayName, Message: f.message, URL: link, Token: p.Token}, undo: f.undo})
		out.Updated = append(out.Updated, chatImportedDTO{Name: p.DisplayName, ParticipantURL: link})
	}
	s.readInBackground(ctx, ws.ID, readings)
	s.Log.Info("chat import", "workshop_id", ws.ID, "imported", len(out.Imported), "updated", len(out.Updated), "skipped", len(out.Skipped))
	writeJSON(w, http.StatusAccepted, out)
	return nil
}

func (s *server) readInBackground(ctx context.Context, workshopID string, people []chatReading) int {
	people = s.chats.startReading(workshopID, people)
	if len(people) == 0 {
		return 0
	}
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatReadTimeout)
	api := inproc.Client{Handler: s.self}
	pace := s.pace()
	s.Background.Add(1)
	go func() {
		defer s.Background.Done()
		defer cancel()
		var wg sync.WaitGroup
		for i, r := range people {
			select {
			case s.chats.slots <- struct{}{}:
			case <-bctx.Done():
				for _, left := range people[i:] {
					s.undoFollowUp(bctx, workshopID, left)
					s.chats.doneReading(workshopID, left.id)
				}
				s.Log.Warn("chat import: stopped before reading everyone", "workshop_id", workshopID, "unread", len(people)-i)
				wg.Wait()
				return
			}
			wg.Go(func() {
				defer func() { <-s.chats.slots }()
				defer s.chats.doneReading(workshopID, r.id)
				if err := demo.AnswerOne(bctx, api, r.p, pace); err != nil {
					s.Log.Warn("chat import: a reading did not arrive", "workshop_id", workshopID, "participant_id", r.id, "error", err)
					s.undoFollowUp(bctx, workshopID, r)
				}
			})
		}
		wg.Wait()
	}()
	return len(people)
}

func (s *server) undoFollowUp(ctx context.Context, workshopID string, r chatReading) {
	if r.undo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatEditTimeout)
	defer cancel()
	restored, err := s.Store.RestoreChatMessage(ctx, r.id, r.undo.wrote, r.undo.previous)
	switch {
	case err != nil:
		s.Log.Error("chat import: the unread new messages could not be offered again", "workshop_id", workshopID, "participant_id", r.id, "error", err)
	case restored:
		s.Log.Info("chat import: the next paste offers the unread new messages again", "workshop_id", workshopID, "participant_id", r.id)
	}
}

func unreadChat(participants []store.ParticipantRow, avails []store.AvailabilityRow) []store.ParticipantRow {
	answered := make(map[string]bool, len(avails))
	for _, a := range avails {
		answered[a.ParticipantID] = true
	}
	var out []store.ParticipantRow
	for _, p := range participants {
		if p.Source == store.SourceChat && !answered[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

func (s *server) retryChat(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	if ws.Status != domain.WorkshopCollecting {
		return errNotCollecting
	}
	ctx, cancel := context.WithTimeout(r.Context(), chatEditTimeout)
	defer cancel()
	participants, err := s.Store.ParticipantsByWorkshop(ctx, ws.ID)
	if err != nil {
		return err
	}
	avails, err := s.Store.AvailabilitiesByWorkshop(ctx, ws.ID)
	if err != nil {
		return err
	}
	var todo []chatReading
	for _, p := range unreadChat(participants, avails) {
		if p.ChatMessage == nil || p.ChatLinkToken == nil || s.chats.isReading(p.ID) {
			continue
		}
		token := *p.ChatLinkToken
		todo = append(todo, chatReading{id: p.ID, p: demo.Participant{
			Name: p.DisplayName, Message: *p.ChatMessage, URL: s.Cfg.BaseURL + "/p/" + token, Token: token,
		}})
	}
	n := 0
	if len(todo) > 0 {
		if err := s.roomToRead(w, ws, len(todo)); err != nil {
			return err
		}
		now := s.Clock.Now()
		if err := s.countRead(w, ws.ID, now); err != nil {
			return err
		}
		if n = s.readInBackground(ctx, ws.ID, todo); n == 0 {
			s.chats.refund(ws.ID, now)
		}
	}
	s.Log.Info("chat import: reading again", "workshop_id", ws.ID, "reading", n)
	writeJSON(w, http.StatusAccepted, map[string]int{"reading": n})
	return nil
}

func (s *server) removeParticipant(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	if s.chats.isReading(id) {
		return errStillReading
	}
	ctx, cancel := context.WithTimeout(r.Context(), chatEditTimeout)
	defer cancel()
	switch err := s.Store.RemoveChatParticipant(ctx, ws.ID, id); {
	case errors.Is(err, store.ErrConflict):
		return errCannotRemove
	case err != nil:
		return err
	}
	s.Log.Info("chat import: participant removed", "workshop_id", ws.ID, "participant_id", id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
