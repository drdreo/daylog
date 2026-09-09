package event

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPresentationBounds(t *testing.T) {
	if err := ValidatePresentation("Local tests passed.\nUI validation remains pending.", []string{"UI untested", "macOS"}); err != nil {
		t.Fatal(err)
	}
	for _, details := range []string{strings.Repeat("ä", MaxDetailsChars+1), "bad\x00text", string([]byte{0xff})} {
		if ValidatePresentation(details, nil) == nil {
			t.Fatal("accepted invalid details")
		}
	}
	for _, tags := range [][]string{{""}, {" padded"}, {"a\nb"}, {"a", "A"}, {strings.Repeat("x", MaxTagChars+1)}, {"a", "b", "c", "d"}} {
		if ValidatePresentation("", tags) == nil {
			t.Fatal("accepted invalid tags", tags)
		}
	}
}

func TestPresentationRoundTripAndHumanCorrection(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)
	base := Event{Version: 2, ID: NewID(time.Now()), PublicationKey: "human/base", Source: "human:cli", Type: TypeNote, TLDR: "Short headline", Details: "Longer explanation; UI remains untested.", Tags: []string{"UI untested"}, RecordedAt: now, OccurredAt: now, TimeBasis: "human"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(base, decoded) {
		t.Fatal("presentation did not survive JSON roundtrip", err)
	}
	entry := Effective([]Event{base})[base.ID]
	if entry.Details != base.Details || !reflect.DeepEqual(entry.Tags, base.Tags) {
		t.Fatal("view lost presentation")
	}
	for _, kind := range []string{TypeAmend, TypeMerge} {
		other := base
		other.ID = NewID(time.Now())
		edit := Event{Type: kind, Source: "human:cli", TLDR: "Corrected wording", Targets: []Target{{base.ID, 1}}}
		if kind == TypeMerge {
			edit.Targets = append(edit.Targets, Target{other.ID, 1})
			edit.Refs = []string{"linear:ABC-1"}
		}
		current := Effective([]Event{base, other, edit})[base.ID]
		if current.Details != "" || len(current.Tags) != 0 || !current.Pinned {
			t.Fatal("human correction retained stale model presentation", kind, current)
		}
		if kind == TypeMerge && len(current.Refs) != 1 {
			t.Fatal("merge dropped selected references")
		}
	}
	base.Type = TypeTodo
	if base.Validate() == nil {
		t.Fatal("narrative presentation accepted on a todo")
	}
}

func TestExistingLongTLDRStillReads(t *testing.T) {
	if err := ValidateTLDR(strings.Repeat("x", 280)); err != nil {
		t.Fatal("historical entries must not inherit the new model headline limit", err)
	}
}
