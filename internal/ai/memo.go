package ai

import (
	"cmp"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const MemoSize = 512

type memoKey [sha256.Size]byte

type memoEntry struct {
	key   memoKey
	value any
	meta  Meta
}

type Memo struct {
	next  Interpreter
	size  int
	mu    sync.Mutex
	order *list.List
	items map[memoKey]*list.Element
}

var _ Interpreter = (*Memo)(nil)

func NewMemo(next Interpreter, size int) *Memo {
	return &Memo{next: next, size: max(size, 1), order: list.New(), items: map[memoKey]*list.Element{}}
}

func (m *Memo) Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
	ids, back := positions(in.Dates, "#")
	norm := in
	norm.Dates = renameOptions(in.Dates, ids)
	return memoize(m, "interpret", norm,
		func() (domain.Interpretation, Meta, error) { return m.next.Interpret(ctx, in) },
		func(out domain.Interpretation) domain.Interpretation { return renameVerdicts(out, ids) },
		func(out domain.Interpretation) domain.Interpretation { return renameVerdicts(out, back) })
}

func (m *Memo) Explain(ctx context.Context, in ExplainInput) (string, Meta, error) {
	return memoize(m, "explain", normalExplain(in),
		func() (string, Meta, error) { return m.next.Explain(ctx, in) }, same[string], same[string])
}

func (m *Memo) Draft(ctx context.Context, in DraftInput) (Draft, Meta, error) {
	norm := in
	y, mo, d := in.Today.Date()
	norm.Today = time.Date(y, mo, d, 0, 0, 0, 0, in.Today.Location())
	return memoize(m, "draft", norm,
		func() (Draft, Meta, error) { return m.next.Draft(ctx, in) }, copyDraft, copyDraft)
}

func (m *Memo) Suggest(ctx context.Context, in SuggestInput) (string, Meta, error) {
	return memoize(m, "suggest", in,
		func() (string, Meta, error) { return m.next.Suggest(ctx, in) }, same[string], same[string])
}

func memoize[T any](m *Memo, op string, key any, call func() (T, Meta, error), save, load func(T) T) (T, Meta, error) {
	raw, err := json.Marshal(key)
	if err != nil {
		return call()
	}
	k := memoKey(sha256.Sum256(append([]byte(op+"\n"), raw...)))
	if value, meta, ok := m.get(k); ok {
		meta.Cached = true
		meta.InputTokens, meta.OutputTokens = 0, 0
		return load(value.(T)), meta, nil
	}
	out, meta, err := call()
	if err == nil {
		m.put(k, save(out), meta)
	}
	return out, meta, err
}

func (m *Memo) get(k memoKey) (any, Meta, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[k]
	if !ok {
		return nil, Meta{}, false
	}
	m.order.MoveToFront(el)
	e := el.Value.(*memoEntry)
	return e.value, e.meta, true
}

func (m *Memo) put(k memoKey, value any, meta Meta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := &memoEntry{key: k, value: value, meta: meta}
	if el, ok := m.items[k]; ok {
		el.Value = entry
		m.order.MoveToFront(el)
		return
	}
	m.items[k] = m.order.PushFront(entry)
	for m.order.Len() > m.size {
		oldest := m.order.Back()
		m.order.Remove(oldest)
		delete(m.items, oldest.Value.(*memoEntry).key)
	}
}

func same[T any](v T) T { return v }

func copyDraft(d Draft) Draft {
	d.Dates = slices.Clone(d.Dates)
	return d
}

func positions(dates []DateOption, prefix string) (ids, back map[string]string) {
	ids = make(map[string]string, len(dates))
	back = make(map[string]string, len(dates))
	for i, d := range dates {
		pos := prefix + strconv.Itoa(i)
		ids[d.ID], back[pos] = pos, d.ID
	}
	return ids, back
}

func rename(names map[string]string, id string) string {
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

func renameOptions(dates []DateOption, ids map[string]string) []DateOption {
	if dates == nil {
		return nil
	}
	out := make([]DateOption, len(dates))
	for i, d := range dates {
		d.ID = rename(ids, d.ID)
		out[i] = d
	}
	return out
}

func renameVerdicts(in domain.Interpretation, ids map[string]string) domain.Interpretation {
	out := in
	out.PerDate = slices.Clone(in.PerDate)
	for i := range out.PerDate {
		out.PerDate[i].CandidateDateID = rename(ids, out.PerDate[i].CandidateDateID)
	}
	out.HardConstraints = slices.Clone(in.HardConstraints)
	out.Preferences = slices.Clone(in.Preferences)
	out.Ambiguities = slices.Clone(in.Ambiguities)
	return out
}

func normalExplain(in ExplainInput) ExplainInput {
	dates, _ := positions(in.Dates, "#")
	ids := slices.SortedFunc(maps.Keys(in.Names), func(a, b string) int {
		return cmp.Or(cmp.Compare(in.Names[a], in.Names[b]), cmp.Compare(a, b))
	})
	people := make(map[string]string, len(ids))
	for i, id := range ids {
		people[id] = "@" + strconv.Itoa(i)
	}
	out := in
	out.Dates = renameOptions(in.Dates, dates)
	out.Names = renameKeys(in.Names, people)
	out.Recommendation.DateID = rename(dates, in.Recommendation.DateID)
	out.Recommendation.PerDate = slices.Clone(in.Recommendation.PerDate)
	for i, s := range out.Recommendation.PerDate {
		s.DateID = rename(dates, s.DateID)
		if s.Compatible != nil {
			compatible := make([]string, len(s.Compatible))
			for j, id := range s.Compatible {
				compatible[j] = rename(people, id)
			}
			s.Compatible = compatible
		}
		s.Blockers = renameKeys(s.Blockers, people)
		out.Recommendation.PerDate[i] = s
	}
	return out
}

func renameKeys(in, names map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for id, value := range in {
		out[rename(names, id)] = value
	}
	return out
}
