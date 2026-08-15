package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shaxzodbek-uzb/pgproof/internal/backup"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/status"
)

// buildStatus lists stored backups and reduces them to one status per database.
func buildStatus(
	ctx context.Context, runner *backup.Runner, cfg *config.Config, dest string, maxAge time.Duration,
) (status.Report, error) {
	entries, err := runner.List(ctx, "", dest)
	if err != nil {
		return status.Report{}, err
	}
	converted := make([]status.Entry, 0, len(entries))
	for _, e := range entries {
		converted = append(converted, status.Entry{
			Database:    e.Database,
			Destination: e.Destination,
			Stamp:       e.Stamp,
			Manifest:    e.Manifest,
		})
	}
	names := make([]string, 0, len(cfg.Databases))
	for _, db := range cfg.Databases {
		names = append(names, db.Name)
	}
	return status.Build(names, converted, maxAge, time.Now().UTC()), nil
}

func statusCmd() *cobra.Command {
	var dest string
	var asJSON bool
	var maxAge time.Duration
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report the latest backup state per database",
		Long: "Summarises, for every configured database, when it was last backed up, whether\n" +
			"that backup passed its restore test, and how old it is.\n\n" +
			"Exits non-zero when any database is unhealthy, so it works directly as a\n" +
			"monitoring check — no parsing required.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, cfg, _, err := load()
			if err != nil {
				return err
			}
			report, err := buildStatus(cmd.Context(), runner, cfg, dest, maxAge)
			if err != nil {
				return err
			}

			if asJSON {
				if err := report.WriteJSON(os.Stdout); err != nil {
					return err
				}
			} else {
				printStatus(report)
			}
			if !report.OK() {
				// A distinct code so a monitoring wrapper can tell an unhealthy
				// backup set apart from pgproof itself failing to run (exit 1).
				os.Exit(2)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "destination to read from (default: first readable)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().DurationVar(&maxAge, "max-age", 0,
		"treat a backup older than this as stale, e.g. 26h (default: no staleness check)")
	return cmd
}

func printStatus(report status.Report) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "DATABASE\tHEALTH\tLAST BACKUP\tAGE\tSIZE\tVERIFY\tBACKUPS")
	for _, db := range report.Databases {
		last, age := "never", "-"
		if db.LastBackup != nil {
			last = db.LastBackup.Local().Format("2006-01-02 15:04")
			age = humanDuration(time.Duration(*db.AgeSeconds) * time.Second)
		}
		size := "-"
		if db.SizeBytes > 0 {
			size = humanBytes(db.SizeBytes)
		}
		verify := db.Verify
		if verify == "" {
			verify = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			db.Database, db.Health, last, age, size, verify, db.Backups)
	}
	_ = w.Flush()

	fmt.Println()
	if report.OK() {
		fmt.Println("✓ all databases have a recent, verified backup")
		return
	}
	fmt.Printf("✗ overall: %s\n", report.Health)
	for _, db := range report.Databases {
		if db.Health == status.HealthOK {
			continue
		}
		fmt.Printf("  %s: %s%s\n", db.Database, explainHealth(db.Health), noteSuffix(db.VerifyNote))
	}
}

func explainHealth(health string) string {
	switch health {
	case status.HealthMissing:
		return "no backups found"
	case status.HealthFailed:
		return "the latest backup FAILED its restore test"
	case status.HealthStale:
		return "the latest backup is older than --max-age"
	case status.HealthUnverif:
		return "the latest backup has not passed a restore test"
	}
	return health
}

func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return " — " + note
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func metricsCmd() *cobra.Command {
	var dest, output string
	var maxAge time.Duration
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Print backup state in the Prometheus text exposition format",
		Long: "Renders the same picture as `status` as Prometheus metrics.\n\n" +
			"Write it to your node_exporter textfile-collector directory from the same cron\n" +
			"entry that takes the backup:\n\n" +
			"  pgproof backup && pgproof metrics -o /var/lib/node_exporter/pgproof.prom\n\n" +
			"For a long-lived deployment, `pgproof run --metrics-addr :9187` serves the same\n" +
			"metrics over HTTP instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runner, cfg, _, err := load()
			if err != nil {
				return err
			}
			report, err := buildStatus(cmd.Context(), runner, cfg, dest, maxAge)
			if err != nil {
				return err
			}
			if output == "" {
				return report.WritePrometheus(os.Stdout)
			}
			return writeAtomically(output, report)
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "destination to read from (default: first readable)")
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"write to this file instead of stdout (for the node_exporter textfile collector)")
	cmd.Flags().DurationVar(&maxAge, "max-age", 0, "treat a backup older than this as stale")
	return cmd
}

// writeAtomically writes via a temp file and renames, so a scrape can never read
// a half-written metrics file.
func writeAtomically(path string, report status.Report) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pgproof-metrics-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op once the rename has succeeded
	}()

	if err := report.WritePrometheus(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// serveMetrics runs an HTTP server exposing /metrics until ctx is cancelled.
//
// The report is rebuilt per scrape rather than cached: it reads manifests from a
// destination, which is cheap relative to a scrape interval, and a cached value
// would keep reporting a healthy backup after someone deleted it.
func serveMetrics(
	ctx context.Context, addr string, build func(context.Context) (status.Report, error),
) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		report, err := build(r.Context())
		if err != nil {
			// Report the scrape as down rather than 500-ing: an alert on
			// pgproof_up is more actionable than a scrape error.
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			fmt.Fprintf(w, "# HELP pgproof_up 1 if the last status check completed.\n")
			fmt.Fprintf(w, "# TYPE pgproof_up gauge\npgproof_up 0\n")
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = report.WritePrometheus(w)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	return srv
}
