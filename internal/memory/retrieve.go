package memory

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/drdreo/daylog/internal/durable"
)

func (s *Store) scoped(owners []string) (*Store, error) {
	if e := validateOwners(owners); e != nil {
		return nil, e
	}
	for _, o := range owners {
		if _, e := s.schema(o); e != nil {
			return nil, e
		}
	}
	r := *s
	r.owners = owners
	return &r, nil
}
func (s *Store) Show(ctx context.Context, owner, id string, allowedOwners []string) (Record, error) {
	if !identifier.MatchString(id) || !allowed(allowedOwners, owner) {
		return Record{}, fmt.Errorf("valid ID and explicit owner scope required")
	}
	r, e := s.scoped(allowedOwners)
	if e != nil {
		return Record{}, e
	}
	return r.active(ctx, nil, owner, id)
}
func (s *Store) History(ctx context.Context, owner, id string, allowedOwners []string) ([]Record, error) {
	scoped, e := s.scoped(allowedOwners)
	if e != nil {
		return nil, e
	}
	current, e := scoped.Show(ctx, owner, id, allowedOwners)
	if e != nil {
		return nil, e
	}
	db, _ := s.schema(owner)
	rows, e := s.db.QueryContext(ctx, "SELECT payload FROM "+db+".records WHERE id=? AND (expires=0 OR expires>?) ORDER BY revision DESC LIMIT 101", id, s.now().UnixNano())
	if e != nil {
		return nil, e
	}
	candidates := []Record{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		var r Record
		if e = durable.Decode([]byte(raw), &r); e != nil {
			rows.Close()
			return nil, e
		}
		candidates = append(candidates, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(candidates) > 100 {
		return nil, fmt.Errorf("history exceeds bounded 100 revisions; narrow history is not supported")
	}
	result := []Record{}
	for _, r := range candidates {
		eligible := true
		for _, ref := range r.Sources {
			if !allowed(allowedOwners, ref.Owner) {
				eligible = false
				break
			}
			source, e := scoped.active(ctx, nil, ref.Owner, ref.ID)
			if e != nil || source.Revision != ref.Revision || source.Hash != ref.Hash {
				eligible = false
				break
			}
		}
		if eligible {
			if r.Revision != current.Revision {
				r.Lifecycle = "corrected"
			}
			result = append(result, r)
		}
	}
	return result, nil
}
func literalQuery(query string) (string, error) {
	if !validText(query, 512, true) {
		return "", fmt.Errorf("query must contain 1..512 bytes of valid text")
	}
	// No FTS operators, quotes, wildcards or SQL syntax are accepted. Terms are
	// quoted by us; AND is literal text rather than user-controlled FTS syntax.
	terms := strings.FieldsFunc(query, func(r rune) bool { return unicode.IsSpace(r) })
	if len(terms) > 16 {
		return "", fmt.Errorf("query exceeds 16 terms")
	}
	for i, t := range terms {
		for _, r := range t {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
				return "", fmt.Errorf("query accepts letters/numbers separated by spaces only")
			}
		}
		terms[i] = "\"" + t + "\""
	}
	return strings.Join(terms, " AND "), nil
}
func (s *Store) Search(ctx context.Context, filter Filter) ([]Record, error) {
	scoped, e := s.scoped(filter.Owners)
	if e != nil {
		return nil, e
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return nil, fmt.Errorf("limit must be 1..100")
	}
	afterOwner, afterID := "", filter.AfterID
	if afterID != "" {
		if len(filter.Owners) == 2 {
			afterOwner, afterID, _ = strings.Cut(afterID, ":")
		} else {
			afterOwner = filter.Owners[0]
		}
		if !identifier.MatchString(afterID) || !allowed(filter.Owners, afterOwner) || filter.Query != "" {
			return nil, fmt.Errorf("after requires ID for one owner or owner:ID for both, and no query")
		}
	}
	if filter.Kind != "" && !member(filter.Kind, "episode thought idea dream mistake lesson memory preference") {
		return nil, fmt.Errorf("invalid kind filter")
	}
	if filter.Status != "" && !member(filter.Status, "reported grounded confirmed inferred speculative disputed") {
		return nil, fmt.Errorf("invalid status filter")
	}
	for _, v := range []string{filter.Project, filter.Topic, filter.Context} {
		if !validText(v, 256, false) {
			return nil, fmt.Errorf("invalid filter")
		}
	}
	match := ""
	if filter.Query != "" {
		match, e = literalQuery(filter.Query)
		if e != nil {
			return nil, e
		}
	}
	queries := []string{}
	args := []any{scoped.now().UnixNano()}
	for _, owner := range filter.Owners {
		db := scoped.schemas[owner]
		score := "0.0"
		join := ""
		if match != "" {
			score = "bm25(search)"
			join = " JOIN " + db + ".search ON search.id=r.id AND CAST(search.revision AS INTEGER)=r.revision"
		}
		q := "SELECT r.payload," + score + " score,'" + owner + "' owner,r.id FROM " + db + ".records r JOIN " + db + ".heads h ON h.id=r.id AND h.revision=r.revision" + join + " WHERE NOT EXISTS(SELECT 1 FROM bad WHERE owner=? AND id=h.id)"
		args = append(args, owner)
		if match != "" {
			q += " AND search MATCH ?"
			args = append(args, match)
		}
		for _, f := range []struct{ name, value string }{{"kind", filter.Kind}, {"status", filter.Status}, {"project", filter.Project}, {"topic", filter.Topic}, {"context", filter.Context}} {
			if f.value != "" {
				q += " AND r." + f.name + "=?"
				args = append(args, f.value)
			}
		}
		if afterID != "" {
			q += " AND (? > ? OR (? = ? AND r.id > ?))"
			args = append(args, owner, afterOwner, owner, afterOwner, afterID)
		}
		queries = append(queries, q)
	}
	q := scoped.eligibleCTE() + strings.Join(queries, " UNION ALL ") + " ORDER BY 2,3,4 LIMIT ?"
	args = append(args, filter.Limit)
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var raw, owner, id string
		var score float64
		if e = rows.Scan(&raw, &score, &owner, &id); e != nil {
			return nil, e
		}
		var r Record
		if e = durable.Decode([]byte(raw), &r); e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
