package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shaxzodbek-uzb/pgproof/internal/backup"
	"github.com/shaxzodbek-uzb/pgproof/internal/catalog"
	"github.com/shaxzodbek-uzb/pgproof/internal/scheduler"
)

func backupCmd() *cobra.Command {
	var only []string
	var prune bool
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Dump, encrypt, upload and verify a backup now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, cfg, _, err := load()
			if err != nil {
				return err
			}
			sum, runErr := runner.BackupAll(cmd.Context(), only)
			if text := sum.Text(); text != "" {
				fmt.Println(text)
			}
			if prune && cfg.Retention.Any() && !sum.Failed() {
				results, perr := runner.Prune(cmd.Context(), only, false)
				if perr != nil {
					return perr
				}
				printPrune(results, false)
			}
			return runErr
		},
	}
	cmd.Flags().StringSliceVar(&only, "db", nil, "only back up these databases (default: all)")
	cmd.Flags().BoolVar(&prune, "prune", false, "apply the retention policy after a successful backup")
	return cmd
}

func verifyCmd() *cobra.Command {
	var db, dest string
	cmd := &cobra.Command{
		Use:   "verify [id]",
		Short: "Prove a stored backup restores (default: the latest)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, _, _, err := load()
			if err != nil {
				return err
			}
			if db == "" {
				return fmt.Errorf("--db is required")
			}
			which := "latest"
			if len(args) == 1 {
				which = args[0]
			}
			rep, err := runner.VerifyExisting(cmd.Context(), db, dest, which)
			if err != nil {
				return err
			}
			if rep.OK {
				fmt.Printf("✓ verified: %s (%s)\n", rep.Note, rep.Duration.Round(time.Millisecond))
				return nil
			}
			return fmt.Errorf("verification FAILED: %s", rep.Note)
		},
	}
	cmd.Flags().StringVar(&db, "db", "", "database name (required)")
	cmd.Flags().StringVar(&dest, "dest", "", "destination to read from (default: first readable)")
	return cmd
}

func restoreCmd() *cobra.Command {
	var db, dest, into, which string
	var clean, yes bool
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a stored backup into a live database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, _, _, err := load()
			if err != nil {
				return err
			}
			if db == "" {
				return fmt.Errorf("--db is required")
			}
			target := into
			if target == "" {
				if d, ok := runner.Config().DatabaseByName(db); ok {
					target = d.DBName
				}
			}
			if !yes {
				ok, err := confirm(fmt.Sprintf("This will restore backup %q of %q INTO database %q. Continue?", which, db, target))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("aborted.")
					return nil
				}
			}
			if err := runner.Restore(cmd.Context(), backup.RestoreOptions{
				Database: db, Dest: dest, Which: which, TargetDB: into, Clean: clean,
			}); err != nil {
				return err
			}
			fmt.Printf("✓ restored %q into %q\n", db, target)
			return nil
		},
	}
	cmd.Flags().StringVar(&db, "db", "", "database name (required)")
	cmd.Flags().StringVar(&dest, "dest", "", "destination to read from (default: first readable)")
	cmd.Flags().StringVar(&into, "into", "", "target database to restore into (default: the configured dbname)")
	cmd.Flags().StringVar(&which, "id", "latest", "backup id to restore (default: latest)")
	cmd.Flags().BoolVar(&clean, "clean", false, "drop existing objects before restoring (postgres custom format only)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

func listCmd() *cobra.Command {
	var db, dest string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored backups and their verification status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, _, _, err := load()
			if err != nil {
				return err
			}
			entries, err := runner.List(cmd.Context(), db, dest)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("no backups found")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "DATABASE\tID\tCREATED\tSIZE\tENCRYPTED\tVERIFY")
			for _, e := range entries {
				enc := "no"
				if e.Manifest.Encrypted {
					enc = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					e.Database,
					catalog.Stamp(e.Stamp),
					e.Stamp.Local().Format("2006-01-02 15:04"),
					humanBytes(e.Manifest.SizeBytes),
					enc,
					e.Manifest.VerifyMark(),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&db, "db", "", "filter to a single database")
	cmd.Flags().StringVar(&dest, "dest", "", "destination to read from (default: first readable)")
	return cmd
}

func pruneCmd() *cobra.Command {
	var only []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old backups per the retention policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, _, _, err := load()
			if err != nil {
				return err
			}
			results, err := runner.Prune(cmd.Context(), only, dryRun)
			if err != nil {
				return err
			}
			printPrune(results, dryRun)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&only, "db", nil, "only prune these databases (default: all)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	return cmd
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Check connectivity and credentials for every destination",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, _, _, err := load()
			if err != nil {
				return err
			}
			results := runner.TestDestinations(cmd.Context())
			failed := false
			for _, r := range results {
				if r.Err != nil {
					failed = true
					fmt.Printf("✗ %s (%s): %v\n", r.Name, r.Type, r.Err)
				} else {
					fmt.Printf("✓ %s (%s): ok\n", r.Name, r.Type)
				}
			}
			if failed {
				return fmt.Errorf("one or more destinations failed their check")
			}
			return nil
		},
	}
	return cmd
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run as a long-lived service, backing up on the configured schedule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, cfg, log, err := load()
			if err != nil {
				return err
			}
			if cfg.Schedule.Cron == "" {
				return fmt.Errorf("`schedule.cron` is not set in the config")
			}
			job := func(ctx context.Context) {
				if _, err := runner.BackupAll(ctx, nil); err != nil {
					log.Error("scheduled backup failed", "error", err)
				}
				if cfg.Schedule.Prune && cfg.Retention.Any() {
					if _, err := runner.Prune(ctx, nil, false); err != nil {
						log.Error("scheduled prune failed", "error", err)
					}
				}
			}
			return scheduler.Run(cmd.Context(), cfg.Schedule.Cron, cfg.Schedule.Timezone, log, job)
		},
	}
	return cmd
}

// confirm reads a y/N answer from stdin.
func confirm(prompt string) (bool, error) {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func printPrune(results []backup.PruneResult, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	for _, r := range results {
		fmt.Printf("%s @ %s: kept %d, %s %d\n", r.Database, r.Destination, r.Kept, verb, len(r.Removed))
		for _, id := range r.Removed {
			fmt.Printf("    - %s\n", id)
		}
		for _, e := range r.Errors {
			fmt.Printf("    ⚠ %s\n", e)
		}
	}
}
