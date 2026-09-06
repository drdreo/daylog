package view

import (
	"sort"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/event"
)

// Month is a compact calendar index of the same entries exposed by Fold.
// Dates use captured display components, never recording partitions or UTC conversion.
type Month struct {
	Version int        `json:"version"`
	Month   string     `json:"month"`
	Days    []DayCount `json:"days"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func CalendarMonth(all []event.Event, month time.Time) Month {
	result := Month{Version: event.Version, Month: month.Format("2006-01"), Days: []DayCount{}}
	counts := map[string]int{}
	for _, e := range event.Effective(all) {
		if !visibleEntry(e) || (e.Type == event.TypeTodo && !e.Done) {
			continue
		}
		if strings.HasPrefix(e.DisplayAt, result.Month+"-") && len(e.DisplayAt) >= 10 {
			counts[e.DisplayAt[:10]]++
		}
	}
	for date, count := range counts {
		result.Days = append(result.Days, DayCount{Date: date, Count: count})
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	return result
}

func visibleEntry(e Entry) bool {
	return !e.Dismissed && e.MergedInto == "" && e.Verdict != event.VerdictDeclined
}
