package athena

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
)

func TestStructuredPublicationAndValidation(t *testing.T) {
	w, f, c := fixture(t, "live")
	in, err := w.input([]capture.Item{{Candidate: c}})
	if err != nil {
		t.Fatal(err)
	}
	good := Action{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "Narrowed the refresh race", Details: "The mutex did not fix it. Cache invalidation still needs investigation.", Tags: []string{"incomplete"}, Reason: "useful diagnosis"}
	for _, change := range []func(*Action){
		func(a *Action) { a.Text = strings.Repeat("a", event.MaxHeadlineChars+1) },
		func(a *Action) { a.Details = strings.Repeat("a", event.MaxDetailsChars+1) },
		func(a *Action) { a.Tags = []string{"a", "b", "c", "d"} },
		func(a *Action) { a.Kind, a.Type, a.Text = "hold", "", "" },
		func(a *Action) { a.Refs = []string{"https://example.com/arbitrary-ui"} },
	} {
		bad := good
		change(&bad)
		if Validate(in, Output{Version: 2, Actions: []Action{bad}}) == nil {
			t.Fatal("invalid presentation accepted", bad)
		}
	}
	f.fn = func(Input) (Output, error) { return Output{Version: 2, Actions: []Action{good}}, nil }
	if _, err := w.Once(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	all, err := store.ReadAll()
	if err != nil || len(all) != 1 || all[0].Details != good.Details || !reflect.DeepEqual(all[0].Tags, good.Tags) {
		t.Fatal("published event lost structured presentation", all, err)
	}
}

func TestRetryIncludesValidationFeedback(t *testing.T) {
	w, f, c := fixture(t, "live")
	f.fn = func(Input) (Output, error) {
		return Output{Version: 2, Actions: []Action{{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: strings.Repeat("x", 101), Reason: "material change"}}}, nil
	}
	if _, err := w.Once(context.Background(), false); err == nil {
		t.Fatal("overlong headline accepted")
	}
	later := w.now().Add(3 * time.Minute)
	w.Now = func() time.Time { return later }
	f.fn = func(in Input) (Output, error) {
		if len(in.PreviousErrors) != 1 || !strings.Contains(in.PreviousErrors[0], "move explanation into details") {
			t.Fatal("retry lacked actionable rejection feedback", in.PreviousErrors)
		}
		return Output{Version: 2, Actions: []Action{{Kind: "publish", Candidates: []string{c.ID}, Type: "work", Text: "Fixed refresh locking", Details: "Longer context belongs here.", Reason: "material change"}}}, nil
	}
	if res, err := w.Once(context.Background(), false); err != nil || res.Events != 1 || f.calls != 2 {
		t.Fatal(res, err, f.calls)
	}
}
