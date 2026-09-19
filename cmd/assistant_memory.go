package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/memory"
	"github.com/spf13/cobra"
)

const memoryInputLimit = 32 * 1024

func init() { rootCmd.AddCommand(newAthenaCommand()) }

func newAthenaCommand() *cobra.Command {
	c := &cobra.Command{Use: "athena", Short: "Inspect and maintain explicitly scoped owner memory"}
	c.AddCommand(newMemoryCommand(), newDreamsCommand())
	return c
}

func newMemoryCommand() *cobra.Command {
	var root string
	c := &cobra.Command{Use: "memory", Short: "Record, recall, and maintain owner-scoped memory"}
	c.PersistentFlags().StringVar(&root, "root", "", "explicit absolute memory root (no default)")
	c.AddCommand(newMemoryWriteCommand(&root, false), newMemoryWriteCommand(&root, true))
	for _, operation := range []string{"list", "recall", "export"} {
		c.AddCommand(newMemorySearchCommand(&root, operation))
	}
	c.AddCommand(newMemoryShowCommand(&root), newMemoryForgetCommand(&root))
	var source string
	rebuild := &cobra.Command{Use: "rebuild", Short: "Rebuild both local derivative search indexes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(source); err != nil {
			return err
		}
		return withMemoryStore(cmd, root, true, nil, func(ctx context.Context, s *memory.Store) error {
			if err := s.Rebuild(ctx); err != nil {
				return err
			}
			return printJSON(cmd, map[string]bool{"rebuilt": true})
		})
	}}
	rebuild.Flags().StringVar(&source, "source", "", "human source convention override (not authentication)")
	c.AddCommand(rebuild)
	return c
}

func withMemoryStore(cmd *cobra.Command, root string, writable bool, owners []string, run func(context.Context, *memory.Store) error) (err error) {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("--root must be an explicit absolute path")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := memory.Open(ctx, root, writable, owners...)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := s.Close(); err == nil {
			err = closeErr
		}
	}()
	return run(ctx, s)
}

func memoryOwner(owner string) error {
	if owner != "athena" && owner != "dreo" {
		return fmt.Errorf("--owner must be athena or dreo")
	}
	return nil
}

func memoryOwners(value string) ([]string, error) {
	owners := strings.Split(value, ",")
	seen := map[string]bool{}
	for _, owner := range owners {
		if err := memoryOwner(owner); err != nil {
			return nil, fmt.Errorf("--owners must list athena and/or dreo, separated by a comma")
		}
		if seen[owner] {
			return nil, fmt.Errorf("--owners must not repeat an owner")
		}
		seen[owner] = true
	}
	return owners, nil
}

func memoryReadScope(cmd *cobra.Command, owner, owners string) ([]string, error) {
	if err := memoryOwner(owner); err != nil {
		return nil, err
	}
	if !cmd.Flags().Changed("owners") {
		return []string{owner}, nil
	}
	allowed, err := memoryOwners(owners)
	if err != nil {
		return nil, err
	}
	for _, candidate := range allowed {
		if candidate == owner {
			return allowed, nil
		}
	}
	return nil, fmt.Errorf("--owners must include the record owner %s", owner)
}

func memoryID(id string) error {
	if !memory.ValidID(id) {
		return fmt.Errorf("ID must contain 1..64 ASCII letters, digits, underscores, or hyphens")
	}
	return nil
}

func readMemoryInput(cmd *cobra.Command, file string) (memory.Input, error) {
	var in memory.Input
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return in, err
	}
	if file == "" {
		return in, fmt.Errorf("--file is required (use - for stdin)")
	}
	reader := cmd.InOrStdin()
	if file != "-" {
		info, err := os.Stat(file)
		if err != nil {
			return in, err
		}
		if !info.Mode().IsRegular() {
			return in, fmt.Errorf("--file must name a regular file; use - for bounded stdin")
		}
		// Nonblocking open also prevents a regular-file-to-FIFO replacement
		// between Stat and Open from hanging on platforms that support FIFOs.
		f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return in, err
		}
		defer f.Close()
		opened, err := f.Stat()
		if err != nil {
			return in, err
		}
		if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			return in, fmt.Errorf("--file changed or is not a regular file")
		}
		reader = f
	}
	data, err := readMemoryBytes(ctx, reader)
	if err != nil {
		return in, err
	}
	if len(data) > memoryInputLimit {
		return in, fmt.Errorf("memory input exceeds 32 KiB")
	}
	if err := durable.Decode(data, &in); err != nil {
		return in, fmt.Errorf("decode memory input: %w", err)
	}
	if err := memoryID(in.ID); err != nil {
		return in, err
	}
	return in, nil
}

// readMemoryBytes never starts a goroutine to read an arbitrary io.Reader:
// cancellation cannot safely stop one. Only known in-memory readers, regular
// files, and OS pipes with working deadlines are accepted.
func readMemoryBytes(ctx context.Context, reader io.Reader) (data []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch input := reader.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader:
	case *os.File:
		info, statErr := input.Stat()
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			if info.Mode()&os.ModeNamedPipe == 0 {
				return nil, fmt.Errorf("stdin must be a regular file or a deadline-capable pipe")
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				return nil, fmt.Errorf("memory input requires a context deadline")
			}
			pipe := input
			if deadlineErr := pipe.SetReadDeadline(deadline); deadlineErr != nil {
				var cleanup func() error
				var prepareErr error
				pipe, cleanup, prepareErr = prepareMemoryPipe(input)
				if prepareErr != nil {
					return nil, prepareErr
				}
				defer func() {
					if cleanupErr := cleanup(); cleanupErr != nil {
						err = errors.Join(err, cleanupErr)
					}
				}()
				if err := pipe.SetReadDeadline(deadline); err != nil {
					return nil, fmt.Errorf("stdin does not support bounded reads; use a regular --file: %w", err)
				}
			}
			reader = pipe
			woken := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				_ = pipe.SetReadDeadline(time.Now())
				close(woken)
			})
			defer func() {
				if !stop() {
					<-woken
				}
				_ = pipe.SetReadDeadline(time.Time{})
			}()
		}
	default:
		return nil, fmt.Errorf("unsupported memory input reader; use an in-memory reader, regular file, or deadline-capable OS pipe")
	}
	data, err = io.ReadAll(io.LimitReader(reader, memoryInputLimit+1))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if os.IsTimeout(err) {
		// A file deadline can fire just before the context timer is scheduled.
		return nil, context.DeadlineExceeded
	}
	return data, err
}

func newMemoryWriteCommand(root *string, correct bool) *cobra.Command {
	var owner, file, source string
	var revision int
	var confirmPreference bool
	use, short, argsCheck := "record", "Record one strict JSON input; ID is the idempotency key", cobra.NoArgs
	if correct {
		use, short, argsCheck = "correct ID", "Replace one memory using its exact current revision", cobra.ExactArgs(1)
	}
	c := &cobra.Command{Use: use, Short: short, Args: argsCheck, RunE: func(cmd *cobra.Command, args []string) error {
		previous := cmd.Context()
		ctx, cancel := context.WithTimeout(previous, 10*time.Second)
		defer cancel()
		cmd.SetContext(ctx)
		defer cmd.SetContext(previous)
		if _, err := humanSource(source); err != nil {
			return err
		}
		if err := memoryOwner(owner); err != nil {
			return err
		}
		if correct {
			if err := memoryID(args[0]); err != nil {
				return err
			}
			if revision < 1 {
				return fmt.Errorf("--revision must be positive")
			}
		}
		in, err := readMemoryInput(cmd, file)
		if err != nil {
			return err
		}
		if in.Owner != owner {
			return fmt.Errorf("input owner must match --owner")
		}
		if correct && in.ID != args[0] {
			return fmt.Errorf("input ID must match the corrected ID")
		}
		return withMemoryStore(cmd, *root, true, nil, func(ctx context.Context, s *memory.Store) error {
			var record memory.Record
			var err error
			if correct {
				record, err = s.Correct(ctx, owner, args[0], revision, in, confirmPreference)
			} else {
				record, err = s.Record(ctx, in, confirmPreference)
			}
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	c.Flags().StringVar(&owner, "owner", "", "required record owner: athena|dreo")
	c.Flags().StringVar(&file, "file", "", "one strict JSON Input file, or - for stdin (maximum 32 KiB)")
	c.Flags().StringVar(&source, "source", "", "human source convention override (not authentication)")
	c.Flags().BoolVar(&confirmPreference, "confirm-preference", false, "explicitly confirm a Dreo-authored, Dreo-owned preference")
	if correct {
		c.Flags().IntVar(&revision, "revision", 0, "required current revision for compare-and-swap")
	}
	return c
}

func addMemoryFilterFlags(c *cobra.Command, f *memory.Filter, kind, after bool) {
	if kind {
		c.Flags().StringVar(&f.Kind, "kind", "", "filter by owner-specific category")
	}
	c.Flags().StringVar(&f.Status, "status", "", "filter by epistemic status")
	c.Flags().StringVar(&f.Project, "project", "", "exact project filter")
	c.Flags().StringVar(&f.Topic, "topic", "", "exact topic filter")
	c.Flags().StringVar(&f.Context, "context", "", "exact context filter")
	c.Flags().IntVar(&f.Limit, "limit", 20, "maximum records in this page (1..100)")
	if after {
		c.Flags().StringVar(&f.AfterID, "after", "", "exclusive ID cursor (owner:ID when --owners includes both)")
	}
}

func validateMemoryPage(f memory.Filter) error {
	if f.Limit < 1 || f.Limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	if f.AfterID != "" {
		id := f.AfterID
		if len(f.Owners) == 2 {
			owner, suffix, found := strings.Cut(id, ":")
			if !found || memoryOwner(owner) != nil {
				return fmt.Errorf("--after requires owner:ID with both owners")
			}
			id = suffix
		}
		if err := memoryID(id); err != nil {
			return err
		}
	}
	return nil
}

func newMemorySearchCommand(root *string, operation string) *cobra.Command {
	var owner, owners string
	var filter memory.Filter
	c := &cobra.Command{Use: operation, Short: "Read a bounded page of current eligible memory as JSON", Args: cobra.NoArgs}
	if operation == "recall" {
		c.Use, c.Args = "recall QUERY", cobra.ExactArgs(1)
		c.Short = "Recall literal search terms in an explicit owner scope"
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("owner") == cmd.Flags().Changed("owners") {
			return fmt.Errorf("specify exactly one of --owner or --owners")
		}
		f := filter
		if cmd.Flags().Changed("owner") {
			if err := memoryOwner(owner); err != nil {
				return err
			}
			f.Owners = []string{owner}
		} else {
			var err error
			f.Owners, err = memoryOwners(owners)
			if err != nil {
				return err
			}
		}
		if operation == "recall" {
			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("recall QUERY must not be empty")
			}
			f.Query = args[0]
		}
		if err := validateMemoryPage(f); err != nil {
			return err
		}
		return withMemoryStore(cmd, *root, false, f.Owners, func(ctx context.Context, s *memory.Store) error {
			records, err := s.Search(ctx, f)
			if err != nil {
				return err
			}
			return printJSON(cmd, records)
		})
	}
	c.Flags().StringVar(&owner, "owner", "", "one owner: athena|dreo (exclusive with --owners)")
	c.Flags().StringVar(&owners, "owners", "", "explicit comma-separated owner scope (exclusive with --owner)")
	addMemoryFilterFlags(c, &filter, true, operation != "recall")
	return c
}

func newMemoryShowCommand(root *string) *cobra.Command {
	var owner, owners string
	var history bool
	c := &cobra.Command{Use: "show ID", Short: "Show current memory, or explicitly request corrected history", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := memoryID(args[0]); err != nil {
			return err
		}
		allowed, err := memoryReadScope(cmd, owner, owners)
		if err != nil {
			return err
		}
		return withMemoryStore(cmd, *root, false, allowed, func(ctx context.Context, s *memory.Store) error {
			if history {
				records, err := s.History(ctx, owner, args[0], allowed)
				if err != nil {
					return err
				}
				return printJSON(cmd, records)
			}
			record, err := s.Show(ctx, owner, args[0], allowed)
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	c.Flags().StringVar(&owner, "owner", "", "required record owner: athena|dreo")
	c.Flags().StringVar(&owners, "owners", "", "explicit allowed owner scope for cited sources; must include --owner")
	c.Flags().BoolVar(&history, "history", false, "include eligible corrected revisions, never forgotten or invalidated text")
	return c
}

func newMemoryForgetCommand(root *string) *cobra.Command {
	var owner, source string
	var revision int
	var confirm bool
	c := &cobra.Command{Use: "forget ID", Short: "Erase local historical text and tracked derivatives; retain tombstones", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(source); err != nil {
			return err
		}
		if err := memoryOwner(owner); err != nil {
			return err
		}
		if err := memoryID(args[0]); err != nil {
			return err
		}
		if revision < 1 {
			return fmt.Errorf("--revision must be positive")
		}
		if !confirm {
			return fmt.Errorf("--confirm required; external copies are not erased")
		}
		return withMemoryStore(cmd, *root, true, nil, func(ctx context.Context, s *memory.Store) error {
			if err := s.Forget(ctx, owner, args[0], revision); err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"owner": owner, "id": args[0], "forgotten": true})
		})
	}}
	c.Flags().StringVar(&owner, "owner", "", "required record owner: athena|dreo")
	c.Flags().StringVar(&source, "source", "", "human source convention override (not authentication)")
	c.Flags().IntVar(&revision, "revision", 0, "required current revision for compare-and-swap")
	c.Flags().BoolVar(&confirm, "confirm", false, "confirm store-local plaintext erasure (not external exports or backups)")
	return c
}

func newDreamsCommand() *cobra.Command {
	var root, owners string
	var filter memory.Filter
	c := &cobra.Command{Use: "dreams [ID]", Short: "Read Athena's stored dreams; never generate or initialize memory", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		allowed, err := memoryReadScope(cmd, "athena", owners)
		if err != nil {
			return err
		}
		f := filter
		f.Kind, f.Owners = "dream", allowed
		if err := validateMemoryPage(f); err != nil {
			return err
		}
		if len(args) == 1 {
			if err := memoryID(args[0]); err != nil {
				return err
			}
			for _, name := range []string{"status", "project", "topic", "context", "limit", "after"} {
				if cmd.Flags().Changed(name) {
					return fmt.Errorf("--%s is only supported when listing dreams", name)
				}
			}
		}
		return withMemoryStore(cmd, root, false, allowed, func(ctx context.Context, s *memory.Store) error {
			if len(args) == 1 {
				record, err := s.Show(ctx, "athena", args[0], allowed)
				if err != nil {
					return err
				}
				if record.Kind != "dream" {
					return fmt.Errorf("record is not an Athena dream")
				}
				return printJSON(cmd, record)
			}
			records, err := s.Search(ctx, f)
			if err != nil {
				return err
			}
			return printJSON(cmd, records)
		})
	}}
	c.Flags().StringVar(&root, "root", "", "explicit absolute memory root (no default)")
	c.Flags().StringVar(&owners, "owners", "", "explicit allowed owner scope for cited sources; must include athena")
	addMemoryFilterFlags(c, &filter, false, true)
	return c
}
