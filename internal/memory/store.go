package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drdreo/daylog/internal/durable"
	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

// Store holds a process lock for its lifetime. All database access uses one
// connection: pragmas cannot accidentally differ on a second pooled connection.
// Writes attach both on-disk databases in DELETE journal mode with FULL sync;
// SQLite's super-journal commits their changes atomically on local filesystems.
// Network filesystems and writers bypassing this package are unsupported.
type Store struct {
	db       *connection
	writable bool
	schemas  map[string]string
	owners   []string
	unlock   func()
	now      func() time.Time
}

// A pinned connection cannot be replaced by database/sql after cancellation:
// losing it fails closed rather than silently losing ATTACH or safety pragmas.
type connection struct {
	*sql.Conn
	pool *sql.DB
}

func (c *connection) Close() error {
	err := c.Conn.Close()
	poolErr := c.pool.Close()
	if err != nil {
		return err
	}
	return poolErr
}

func regularPrivate(path string, directory bool) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("memory path is not a regular private file/directory: %s", path)
	}
	if e = checkLinks(info); e != nil {
		return e
	}
	return checkPrivate(path, info, directory)
}
func Open(ctx context.Context, root string, writable bool, owners ...string) (_ *Store, err error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("explicit clean absolute memory root required")
	}
	if writable {
		owners = []string{"athena", "dreo"}
	} else if e := validateOwners(owners); e != nil {
		return nil, e
	}
	if writable {
		if _, e := os.Lstat(root); os.IsNotExist(e) {
			if e = durable.Mkdir(root); e != nil {
				return nil, e
			}
		}
	}
	if e := regularPrivate(root, true); e != nil {
		return nil, e
	}
	// A fresh memory namespace must not silently adopt unrelated files. Reads
	// inspect only named, explicitly scoped databases and never enumerate content.
	if writable {
		entries, e := os.ReadDir(root)
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			name := entry.Name()
			known := name == "memory.lock"
			for _, owner := range owners {
				base := owner + ".sqlite"
				known = known || name == base || name == base+"-journal" || name == base+"-wal" || name == base+"-shm" || strings.HasPrefix(name, base+"-mj")
			}
			if !known {
				return nil, fmt.Errorf("refuse unrelated file in memory root")
			}
			if e := regularPrivate(filepath.Join(root, name), false); e != nil {
				return nil, e
			}
		}
	}
	lockPath := filepath.Join(root, "memory.lock")
	if writable {
		if _, e := os.Lstat(lockPath); os.IsNotExist(e) {
			f, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if e != nil && !os.IsExist(e) {
				return nil, e
			}
			if e == nil {
				if e = durable.Private(lockPath, false); e != nil {
					f.Close()
					return nil, e
				}
				if e = f.Close(); e != nil {
					return nil, e
				}
			}
		}
	}
	if e := regularPrivate(lockPath, false); e != nil {
		return nil, e
	}
	lock := flock.New(lockPath)
	var ok bool
	if writable {
		ok, err = lock.TryLock()
	} else {
		ok, err = lock.TryRLock()
	}
	if err != nil || !ok {
		lock.Close()
		if err == nil {
			err = fmt.Errorf("memory store busy")
		}
		return nil, err
	}
	s := &Store{writable: writable, schemas: map[string]string{}, owners: append([]string(nil), owners...), unlock: func() { _ = lock.Unlock(); _ = lock.Close() }, now: time.Now}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	for _, owner := range owners {
		path := filepath.Join(root, owner+".sqlite")
		if writable {
			if _, e := os.Lstat(path); os.IsNotExist(e) {
				f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
				if e != nil {
					return nil, e
				}
				e = durable.Private(path, false)
				ce := f.Close()
				if e != nil {
					return nil, e
				}
				if ce != nil {
					return nil, ce
				}
			}
		}
		if e := regularPrivate(path, false); e != nil {
			return nil, e
		}
		for _, suffix := range []string{"-journal", "-wal", "-shm"} {
			side := path + suffix
			if _, e := os.Lstat(side); e == nil {
				if e = regularPrivate(side, false); e != nil {
					return nil, e
				}
				if suffix != "-journal" {
					return nil, fmt.Errorf("WAL/SHM memory stores are unsupported; inspect existing sidecars")
				}
			} else if !os.IsNotExist(e) {
				return nil, e
			}
		}
	}
	mode := "ro"
	if writable {
		mode = "rw"
	}
	uri := func(owner string) string {
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, owner+".sqlite"))}
		q := url.Values{"mode": {mode}, "_pragma": {"busy_timeout(250)", "foreign_keys(ON)"}}
		u.RawQuery = q.Encode()
		return u.String()
	}
	pool, err := sql.Open("sqlite", uri(owners[0]))
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	conn, err := pool.Conn(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	s.db = &connection{Conn: conn, pool: pool}
	s.schemas[owners[0]] = "main"
	if err = s.db.PingContext(ctx); err != nil {
		return nil, err
	}
	if len(owners) == 2 {
		s.schemas[owners[1]] = "peer"
		if _, err = s.db.ExecContext(ctx, "ATTACH DATABASE ? AS peer", uri(owners[1])); err != nil {
			return nil, err
		}
	}
	for _, owner := range owners {
		schema := s.schemas[owner]
		var version, application, tables int
		if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".user_version").Scan(&version); err != nil {
			return nil, err
		}
		if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".application_id").Scan(&application); err != nil {
			return nil, err
		}
		if version == 0 && writable {
			if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".sqlite_master").Scan(&tables); err != nil {
				return nil, err
			}
			if tables != 0 || application != 0 {
				return nil, fmt.Errorf("refuse unrelated database")
			}
		} else if version != 1 || application != 1145914701 {
			return nil, fmt.Errorf("unsupported memory database identity/version")
		}
		var existingMode string
		if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".journal_mode").Scan(&existingMode); err != nil {
			return nil, err
		}
		if existingMode != "delete" {
			return nil, fmt.Errorf("unsupported journal mode; no automatic conversion")
		}
		if writable {
			var journal string
			if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".journal_mode=DELETE").Scan(&journal); err != nil {
				return nil, err
			}
			if journal != "delete" {
				return nil, fmt.Errorf("rollback journal required")
			}
			for _, pragma := range []string{"synchronous=FULL", "secure_delete=ON"} {
				if _, err = s.db.ExecContext(ctx, "PRAGMA "+schema+"."+pragma); err != nil {
					return nil, err
				}
			}
		}
		var journal string
		var sync int
		if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".journal_mode").Scan(&journal); err != nil {
			return nil, err
		}
		if journal != "delete" {
			return nil, fmt.Errorf("unsupported journal mode")
		}
		if writable {
			if err = s.db.QueryRowContext(ctx, "PRAGMA "+schema+".synchronous").Scan(&sync); err != nil {
				return nil, err
			}
			if sync != 2 {
				return nil, fmt.Errorf("FULL synchronous required")
			}
		}
	}
	if writable {
		if err = s.initialize(ctx); err != nil {
			return nil, err
		}
		if err = durable.SyncDir(root); err != nil {
			return nil, err
		}
	}
	for _, owner := range owners {
		var version int
		if err = s.db.QueryRowContext(ctx, "PRAGMA "+s.schemas[owner]+".user_version").Scan(&version); err != nil {
			return nil, err
		}
		if version != 1 {
			return nil, fmt.Errorf("unsupported memory schema")
		}
	}
	if !writable {
		_, err = s.db.ExecContext(ctx, "PRAGMA query_only=ON")
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
		s.db = nil
	}
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
	return err
}
func (s *Store) initialize(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, owner := range s.owners {
		db := s.schemas[owner]
		var version, count int
		if e = tx.QueryRowContext(ctx, "PRAGMA "+db+".user_version").Scan(&version); e != nil {
			return e
		}
		if version == 1 {
			continue
		}
		if version != 0 {
			return fmt.Errorf("unsupported memory schema")
		}
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+db+".sqlite_master").Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return fmt.Errorf("refuse unversioned nonempty database")
		}
		statements := []string{
			"CREATE TABLE %s.heads (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, lifecycle TEXT NOT NULL CHECK(lifecycle IN ('active','invalidated','forgotten')))",
			"CREATE TABLE %s.records (id TEXT NOT NULL, revision INTEGER NOT NULL, hash TEXT NOT NULL, payload TEXT NOT NULL, expires INTEGER NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL, project TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL, PRIMARY KEY(id,revision))",
			"CREATE INDEX %s.category_lookup ON records(kind,status,project,topic,context)",
			"CREATE TABLE %s.sources (id TEXT NOT NULL, revision INTEGER NOT NULL, owner TEXT NOT NULL, source_id TEXT NOT NULL, source_revision INTEGER NOT NULL, hash TEXT NOT NULL, PRIMARY KEY(id,revision,owner,source_id))",
			"CREATE INDEX %s.source_lookup ON sources(owner,source_id)",
			"PRAGMA %s.application_id=1145914701",
			"PRAGMA %s.user_version=1",
		}
		for _, q := range statements {
			if _, e = tx.ExecContext(ctx, fmt.Sprintf(q, db)); e != nil {
				return e
			}
		}
		if e = createSearch(ctx, tx, db); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func createSearch(ctx context.Context, tx *sql.Tx, schema string) error {
	if _, err := tx.ExecContext(ctx, "CREATE VIRTUAL TABLE "+schema+".search USING fts5(id UNINDEXED, revision UNINDEXED, content, tokenize='unicode61')"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".search(search,rank) VALUES('secure-delete',1)")
	return err
}
func (s *Store) requireWrite() error {
	if !s.writable {
		return fmt.Errorf("read-only memory store")
	}
	return nil
}
func (s *Store) schema(owner string) (string, error) {
	if _, e := ownerDB(owner); e != nil {
		return "", e
	}
	db, ok := s.schemas[owner]
	if !ok {
		return "", fmt.Errorf("owner outside explicit read scope")
	}
	return db, nil
}

// eligibleCTE excludes missing, stale, expired and invalid sources recursively.
// Only metadata from explicitly opened owners participates in read operations.
func (s *Store) eligibleCTE() string {
	heads, edges := []string{}, []string{}
	for _, owner := range s.owners {
		db := s.schemas[owner]
		heads = append(heads, fmt.Sprintf("SELECT '%s' owner,h.id,h.revision,h.lifecycle,r.hash,r.expires FROM %s.heads h LEFT JOIN %s.records r ON r.id=h.id AND r.revision=h.revision", owner, db, db))
		edges = append(edges, fmt.Sprintf("SELECT '%s' owner,s.id,s.revision,s.owner source_owner,s.source_id,s.source_revision,s.hash FROM %s.sources s JOIN %s.heads h ON h.id=s.id AND h.revision=s.revision", owner, db, db))
	}
	return "WITH RECURSIVE current AS (" + strings.Join(heads, " UNION ALL ") + "), edges AS (" + strings.Join(edges, " UNION ALL ") + "), bad(owner,id) AS (SELECT owner,id FROM current WHERE lifecycle!='active' OR hash IS NULL OR (expires!=0 AND expires<=?) UNION SELECT e.owner,e.id FROM edges e LEFT JOIN current c ON c.owner=e.source_owner AND c.id=e.source_id WHERE c.id IS NULL OR c.revision!=e.source_revision OR c.hash!=e.hash UNION SELECT e.owner,e.id FROM edges e JOIN bad b ON b.owner=e.source_owner AND b.id=e.source_id) "
}
func (s *Store) active(ctx context.Context, tx *sql.Tx, owner, id string) (Record, error) {
	db, e := s.schema(owner)
	if e != nil {
		return Record{}, e
	}
	q := s.eligibleCTE() + "SELECT r.payload FROM " + db + ".records r JOIN " + db + ".heads h ON h.id=r.id AND h.revision=r.revision WHERE h.id=? AND NOT EXISTS(SELECT 1 FROM bad WHERE owner=? AND id=h.id)"
	var raw string
	if tx != nil {
		e = tx.QueryRowContext(ctx, q, s.now().UnixNano(), id, owner).Scan(&raw)
	} else {
		e = s.db.QueryRowContext(ctx, q, s.now().UnixNano(), id, owner).Scan(&raw)
	}
	if errors.Is(e, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("record unavailable (missing, expired, stale or withdrawn)")
	}
	if e != nil {
		return Record{}, e
	}
	var r Record
	if e = durable.Decode([]byte(raw), &r); e != nil {
		return Record{}, e
	}
	return r, nil
}
