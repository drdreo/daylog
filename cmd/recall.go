package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
)

const recallMaxBytes = 48 * 1024

// This is a read response, not a persisted store or event schema.
type projectRecall struct {
	Version    int                  `json:"version"`
	Project    event.Repository     `json:"project"`
	ObservedAt string               `json:"observed_at"`
	Entries    []recallJournalEntry `json:"entries"`
	Reports    []recallReport       `json:"reports"`
	Coverage   recallCoverage       `json:"coverage"`
}
type recallJournalEntry struct {
	event.Entry
	RecordedEvents []recallRecorded `json:"recorded_events"`
}
type recallCoverage struct {
	Limit           int      `json:"limit_per_collection"`
	EntryMatches    int      `json:"entry_matches"`
	ReportMatches   int      `json:"report_matches"`
	EntriesOmitted  int      `json:"entries_omitted"`
	ReportsOmitted  int      `json:"reports_omitted"`
	EntryOrder      string   `json:"entry_order"`
	ReportOrder     string   `json:"report_order"`
	AtomicSnapshot  bool     `json:"atomic_snapshot"`
	CompleteHistory bool     `json:"complete_history"`
	Exclusions      []string `json:"exclusions"`
}
type recallReport struct {
	capture.Item
	PublicationState string           `json:"publication_state"`
	RecordedEvents   []recallRecorded `json:"recorded_events"`
}
type recallRecorded struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Type       string   `json:"type"`
	RecordedAt string   `json:"recorded_at"`
	OccurredAt string   `json:"occurred_at"`
	EntryIDs   []string `json:"entry_ids"`
}

var recallProjectPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

func recallProject(value string) (event.Repository, error) {
	if len(value) > 512 || !recallProjectPattern.MatchString(value) {
		return event.Repository{}, fmt.Errorf("--project requires an exact HOST/OWNER/REPO identity; no URL, alias, path, or inferred scope")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return event.Repository{}, fmt.Errorf("invalid --project identity")
		}
	}
	return event.Repository{Host: parts[0], Path: parts[1] + "/" + parts[2]}, nil
}

func readProjectRecall(repo event.Repository, limit int) (projectRecall, error) {
	result := projectRecall{
		Version: 1, Project: repo, ObservedAt: time.Now().Format(time.RFC3339Nano),
		Entries: []recallJournalEntry{}, Reports: []recallReport{},
		Coverage: recallCoverage{Limit: limit,
			EntryOrder: "display_at_desc_id_asc", ReportOrder: "occurred_at_desc_id_asc",
			Exclusions: []string{"missing_or_other_repository_identity", "todos", "github_snapshots", "native_evidence_excerpts", "plans", "transcripts", "uncaptured_work"}},
	}
	root, err := store.DataDir()
	if err != nil {
		return result, fmt.Errorf("project recall store unavailable; evidence withheld")
	}
	// The shared ledger reader interpolates the root into a Glob pattern.
	// Reject pattern-sensitive roots before reading, rather than silently
	// omitting the real ledger or reading a matching sibling store. Backslash
	// is an escape on Unix, but a literal separator in Windows Glob patterns.
	if strings.ContainsAny(root, "*?[") || (os.PathSeparator != '\\' && strings.Contains(root, `\`)) {
		return result, fmt.Errorf("project recall does not support glob-sensitive store paths; evidence withheld")
	}
	// Reuse the existing validated, locked ledger read. Never initialize a store,
	// open a writer spool, load plans/evidence, or invoke the curation worker.
	all, err := store.ReadAllExisting()
	if err != nil {
		return result, fmt.Errorf("project recall ledger read failed; evidence withheld")
	}
	// The existing ledger reader uses Glob, which can hide directory read
	// errors. Distinguish an absent (empty) journal from an unreadable one.
	if _, err := os.ReadDir(filepath.Join(root, "events")); err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("project recall ledger directory read failed; evidence withheld")
	}
	q := &capture.Spool{Root: filepath.Join(root, "capture")}
	items, err := q.ListProject(repo)
	if err != nil {
		return result, fmt.Errorf("project recall queue read failed; evidence withheld")
	}
	current := event.Effective(all)
	for _, entry := range current {
		if entry.Context.Repository == repo && event.Narrative(entry.Type) {
			// Retain dismissed/merged identities and their current flags, rather
			// than laundering an old report into a still-active accomplishment.
			result.Entries = append(result.Entries, recallJournalEntry{Entry: entry, RecordedEvents: []recallRecorded{}})
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		return recallNewer(a.DisplayAt, a.ID, b.DisplayAt, b.ID)
	})
	result.Coverage.EntryMatches = len(result.Entries)
	if len(result.Entries) > limit {
		result.Entries = result.Entries[:limit]
	}
	// Reports remain separate even when published: preserve processing state,
	// and expose recorded links so callers do not count two independent outcomes.
	// Provenance also catches append-before-receipt-ack crashes. A receipt alone
	// never certifies a write; only a matching event in this ledger read does.
	byCandidate := map[string][]recallRecorded{}
	byEvent := map[string]recallRecorded{}
	entryIndexes := map[string]int{}
	for i, entry := range result.Entries {
		entryIndexes[entry.ID] = i
	}
	for _, e := range all {
		ids := []string{}
		if len(e.Targets) == 0 {
			ids = append(ids, e.ID)
		} else {
			for _, target := range e.Targets {
				ids = append(ids, target.ID)
			}
		}
		// Corrections retain the target's project, not a correcting shell's cwd.
		// Reject links that include any target outside the selected repository.
		inScope := len(ids) > 0
		hasSelectedTarget := false
		for _, id := range ids {
			entry, ok := current[id]
			if !ok || entry.Context.Repository != repo || !event.Narrative(entry.Type) {
				inScope = false
			} else {
				hasSelectedTarget = true
			}
		}
		if !inScope {
			// Existing merges can use a cwd fallback for repository-less targets.
			// Recall must not adopt that fallback or emit merged wording whose
			// full attribution cannot stay within the explicit repository scope.
			if hasSelectedTarget {
				return result, fmt.Errorf("project recall history crosses repository identities; evidence withheld")
			}
			continue
		}
		link := recallRecorded{e.ID, e.Source, e.Type, e.RecordedAt, e.OccurredAt, ids}
		byEvent[e.ID] = link
		// Folded entries retain the original event source/timestamps even when
		// their wording or suppression state changes. Preserve all affecting
		// event metadata in ledger order, including human corrections with no
		// candidate provenance or receipt link.
		for _, id := range ids {
			if i, ok := entryIndexes[id]; ok {
				result.Entries[i].RecordedEvents = append(result.Entries[i].RecordedEvents, link)
			}
		}
		if e.Provenance != nil {
			for _, id := range e.Provenance.Candidates {
				byCandidate[id] = append(byCandidate[id], link)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Candidate, items[j].Candidate
		return recallNewer(a.OccurredAt, a.ID, b.OccurredAt, b.ID)
	})
	result.Coverage.ReportMatches = len(items)
	if len(items) > limit {
		items = items[:limit]
	}
	for _, item := range items {
		r := recallReport{Item: item, PublicationState: "not_observed", RecordedEvents: []recallRecorded{}}
		seen := map[string]bool{}
		links := append([]recallRecorded{}, byCandidate[item.Candidate.ID]...)
		for _, id := range item.Receipt.AppliedEvents {
			if link, ok := byEvent[id]; ok {
				links = append(links, link)
			}
		}
		for _, link := range links {
			if !seen[link.ID] {
				r.RecordedEvents = append(r.RecordedEvents, link)
				seen[link.ID] = true
			}
		}
		sort.Slice(r.RecordedEvents, func(i, j int) bool { return r.RecordedEvents[i].ID < r.RecordedEvents[j].ID })
		if len(r.RecordedEvents) > 0 {
			r.PublicationState = "recorded"
		}
		result.Reports = append(result.Reports, r)
	}
	result.Coverage.EntriesOmitted = result.Coverage.EntryMatches - len(result.Entries)
	result.Coverage.ReportsOmitted = result.Coverage.ReportMatches - len(result.Reports)
	return result, nil
}

func recallNewer(a, aid, b, bid string) bool {
	at, _ := time.Parse(time.RFC3339Nano, a)
	bt, _ := time.Parse(time.RFC3339Nano, b)
	if at.Equal(bt) {
		return aid < bid
	}
	return at.After(bt)
}

func init() {
	var projects []string
	var limit int
	c := &cobra.Command{
		Use:   "recall --project HOST/OWNER/REPO [--limit 5]",
		Short: "Read bounded project journal and report evidence as JSON",
		Long: `Read current narrative entries (including dismissed/merged identities) and
private candidate/receipt metadata for exactly context.repository.host/path.
--project is mandatory, case-sensitive, and never inferred from cwd or refs.
Missing repository identities are excluded; there is no path or all-project fallback.
JSON only; limit is 1..5 per collection (default 5), with a 48 KiB total output cap.
Orders are reported occurrence/display time descending, then ID ascending, not
current truth or publication authority. Reports include all processing states;
processed/hold/skip is not publication. recorded_events links observed ledger
writes, not independent outcomes or proof of execution. Entries also retain all
affecting event IDs, sources and timestamps in ledger order, including corrections.
Store paths containing glob metacharacters are unsupported and fail closed.
Mixed-repository-identity history affecting this project also fails closed.
No native excerpts,
plans, transcripts, curation, initialization, network, or snapshot reads.
The existing ledger and candidate index are read locally before native filtering;
limits bound returned evidence, not disk scans. Reads are not an atomic snapshot.
Any read/size failure withholds all evidence; empty success is not absent work.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(projects) != 1 {
				return fmt.Errorf("exactly one explicit --project is required")
			}
			repo, err := recallProject(projects[0])
			if err != nil {
				return err
			}
			if limit < 1 || limit > 5 {
				return fmt.Errorf("--limit must be 1..5 per collection")
			}
			result, err := readProjectRecall(repo, limit)
			if err != nil {
				return err
			}
			data, err := json.Marshal(result)
			if err != nil {
				return fmt.Errorf("project recall encoding failed; evidence withheld")
			}
			if len(data)+1 > recallMaxBytes {
				return fmt.Errorf("project recall exceeds 48 KiB; evidence withheld; request a smaller --limit")
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		},
	}
	c.Flags().StringArrayVar(&projects, "project", nil, "exact captured repository identity HOST/OWNER/REPO (exactly one required)")
	c.Flags().IntVar(&limit, "limit", 5, "maximum records per collection, 1..5")
	rootCmd.AddCommand(c)
}
