package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) Record(ctx context.Context, in Input, confirmPreference bool) (Record, error) {
	return s.put(ctx, in, 0, confirmPreference)
}
func (s *Store) Correct(ctx context.Context, owner, id string, revision int, in Input, confirmPreference bool) (Record, error) {
	if revision < 1 || in.Owner != owner || in.ID != id {
		return Record{}, fmt.Errorf("correction requires matching owner/ID and positive expected revision")
	}
	return s.put(ctx, in, revision, confirmPreference)
}
func (s *Store) put(ctx context.Context, in Input, expected int, confirm bool) (Record, error) {
	if e := s.requireWrite(); e != nil {
		return Record{}, e
	}
	if e := validateInput(in, confirm); e != nil {
		return Record{}, e
	}
	if ex := expiresUnix(in); ex != 0 && ex <= s.now().UnixNano() {
		return Record{}, fmt.Errorf("cannot write expired input")
	}
	db, e := s.schema(in.Owner)
	if e != nil {
		return Record{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Record{}, e
	}
	defer tx.Rollback()
	var revision int
	var lifecycle string
	e = tx.QueryRowContext(ctx, "SELECT revision,lifecycle FROM "+db+".heads WHERE id=?", in.ID).Scan(&revision, &lifecycle)
	if e != nil && e != sql.ErrNoRows {
		return Record{}, e
	}
	hash := inputHash(in)
	if expected == 0 && e == nil {
		r, e := s.active(ctx, tx, in.Owner, in.ID)
		if e == nil && r.Hash == hash {
			return r, nil
		}
		return Record{}, fmt.Errorf("ID already exists with conflicting or withdrawn content")
	}
	if expected > 0 && (e != nil || revision != expected || lifecycle != "active") {
		return Record{}, fmt.Errorf("stale revision or inactive record")
	}
	if expected > 0 {
		if _, e = s.active(ctx, tx, in.Owner, in.ID); e != nil {
			return Record{}, e
		}
	}
	// Every source is pinned to a current revision/hash before any write. Sources
	// are explicit structured data; nothing in Content or Provenance is executed.
	for _, ref := range in.Sources {
		r, e := s.active(ctx, tx, ref.Owner, ref.ID)
		if e != nil {
			return Record{}, fmt.Errorf("source unavailable: %w", e)
		}
		if r.Revision != ref.Revision || r.Hash != ref.Hash {
			return Record{}, fmt.Errorf("stale source citation")
		}
		descendants, e := s.dependents(ctx, tx, in.Owner, in.ID)
		if e != nil {
			return Record{}, e
		}
		for _, d := range descendants {
			if d.Owner == ref.Owner && d.ID == ref.ID {
				return Record{}, fmt.Errorf("cyclic source dependency")
			}
		}
	}
	revision = expected + 1
	r := Record{Input: in, Revision: revision, Hash: hash, Lifecycle: "active", RecordedAt: s.now().UTC().Format(time.RFC3339Nano)}
	raw, e := json.Marshal(r)
	if e != nil {
		return Record{}, e
	}
	if expected > 0 {
		if e = s.invalidate(ctx, tx, in.Owner, in.ID, false); e != nil {
			return Record{}, e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+db+".records(id,revision,hash,payload,expires,kind,status,project,topic,context) VALUES(?,?,?,?,?,?,?,?,?,?)", in.ID, revision, hash, string(raw), expiresUnix(in), in.Kind, in.Status, in.Project, in.Topic, in.Context); e != nil {
		return Record{}, e
	}
	if expected == 0 {
		_, e = tx.ExecContext(ctx, "INSERT INTO "+db+".heads(id,revision,lifecycle) VALUES(?,?,'active')", in.ID, revision)
	} else {
		var result sql.Result
		result, e = tx.ExecContext(ctx, "UPDATE "+db+".heads SET revision=? WHERE id=? AND revision=? AND lifecycle='active'", revision, in.ID, expected)
		if e == nil {
			n, err := result.RowsAffected()
			e = err
			if n != 1 && e == nil {
				e = fmt.Errorf("stale revision")
			}
		}
	}
	if e != nil {
		return Record{}, e
	}
	for _, ref := range in.Sources {
		if _, e = tx.ExecContext(ctx, "INSERT INTO "+db+".sources VALUES(?,?,?,?,?,?)", in.ID, revision, ref.Owner, ref.ID, ref.Revision, ref.Hash); e != nil {
			return Record{}, e
		}
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM "+db+".search WHERE id=?", in.ID); e != nil {
		return Record{}, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+db+".search(id,revision,content) VALUES(?,?,?)", in.ID, revision, in.Content); e != nil {
		return Record{}, e
	}
	if e = tx.Commit(); e != nil {
		return Record{}, e
	}
	return r, nil
}

// dependents follows all retained revisions. This is deliberately conservative:
// forgetting an old source also removes a corrected artifact that once used it.
// Only Athena-derived artifacts can have source edges (validateInput).
func (s *Store) dependents(ctx context.Context, tx *sql.Tx, owner, id string) ([]Reference, error) {
	db, e := s.schema("athena")
	if e != nil {
		return nil, e
	}
	q := "WITH RECURSIVE affected(owner,id) AS (SELECT owner,source_id FROM " + db + ".sources WHERE owner=? AND source_id=? UNION SELECT 'athena',s.id FROM " + db + ".sources s JOIN affected a ON s.owner=a.owner AND s.source_id=a.id) SELECT owner,id FROM affected WHERE NOT(owner=? AND id=?) ORDER BY owner,id"
	rows, e := tx.QueryContext(ctx, q, owner, id, owner, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []Reference{}
	for rows.Next() {
		var r Reference
		if e = rows.Scan(&r.Owner, &r.ID); e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Store) invalidate(ctx context.Context, tx *sql.Tx, owner, id string, forget bool) error {
	affected, e := s.dependents(ctx, tx, owner, id)
	if e != nil {
		return e
	}
	if forget {
		affected = append(affected, Reference{Owner: owner, ID: id})
	}
	for _, r := range affected {
		db, e := s.schema(r.Owner)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+db+".search WHERE id=?", r.ID); e != nil {
			return e
		}
		if forget {
			for _, table := range []string{"records", "sources"} {
				if _, e = tx.ExecContext(ctx, "DELETE FROM "+db+"."+table+" WHERE id=?", r.ID); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+db+".heads SET lifecycle='forgotten',revision=revision+1 WHERE id=? AND lifecycle!='forgotten'", r.ID); e != nil {
				return e
			}
		} else {
			if _, e = tx.ExecContext(ctx, "UPDATE "+db+".heads SET lifecycle='invalidated' WHERE id=? AND lifecycle='active'", r.ID); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) Forget(ctx context.Context, owner, id string, revision int) error {
	if e := s.requireWrite(); e != nil {
		return e
	}
	if !ValidID(id) || revision < 1 {
		return fmt.Errorf("valid ID and positive expected revision required")
	}
	db, e := s.schema(owner)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var current int
	var state string
	if e = tx.QueryRowContext(ctx, "SELECT revision,lifecycle FROM "+db+".heads WHERE id=?", id).Scan(&current, &state); e != nil {
		return e
	}
	if state == "forgotten" && current == revision+1 {
		return nil
	}
	if current != revision || state == "forgotten" {
		return fmt.Errorf("stale revision")
	}
	if e = s.invalidate(ctx, tx, owner, id, true); e != nil {
		return e
	}
	// Remove any remaining inbound references, including old revision metadata.
	for _, schema := range s.schemas {
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+schema+".sources WHERE owner=? AND source_id=?", owner, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Rebuild(ctx context.Context) error {
	if e := s.requireWrite(); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, owner := range s.owners {
		db := s.schemas[owner]
		if _, e = tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+db+".search"); e != nil {
			return e
		}
		if e = createSearch(ctx, tx, db); e != nil {
			return e
		}
		q := s.eligibleCTE() + "INSERT INTO " + db + ".search(id,revision,content) SELECT r.id,r.revision,json_extract(r.payload,'$.content') FROM " + db + ".records r JOIN " + db + ".heads h ON h.id=r.id AND h.revision=r.revision WHERE NOT EXISTS(SELECT 1 FROM bad WHERE owner=? AND id=h.id)"
		if _, e = tx.ExecContext(ctx, q, s.now().UnixNano(), owner); e != nil {
			return e
		}
	}
	return tx.Commit()
}
