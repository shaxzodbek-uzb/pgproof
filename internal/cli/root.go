// Package cli wires the cobra command tree for pgproof.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shaxzodbek-uzb/pgproof/internal/backup"
	"github.com/shaxzodbek-uzb/pgproof/internal/buildinfo"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
	"github.com/shaxzodbek-uzb/pgproof/internal/logging"
)

var (
	flagConfig    string
	flagLogLevel  string
	flagLogFormat string
)

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pgproof",
		Short: "Postgres & MySQL backups you've proven restore",
		Long: "pgproof dumps your databases, encrypts them, ships them to S3/R2/local/Telegram,\n" +
			"and — the part most tools skip — proves each backup actually restores by replaying\n" +
			"it into a throwaway database. Failures page you on Telegram before you need the backup.",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate(buildinfo.String() + "\n")

	root.PersistentFlags().StringVarP(&flagConfig, "config", "c", defaultConfigPath(), "path to the pgproof config file")
	root.PersistentFlags().StringVar(&flagLogLevel, "log-level", "", "override log level (debug|info|warn|error)")
	root.PersistentFlags().StringVar(&flagLogFormat, "log-format", "", "override log format (text|json)")

	root.AddCommand(
		initCmd(),
		backupCmd(),
		verifyCmd(),
		restoreCmd(),
		listCmd(),
		pruneCmd(),
		runCmd(),
		testCmd(),
		keygenCmd(),
	)
	return root
}

func defaultConfigPath() string {
	if p := os.Getenv("PGPROOF_CONFIG"); p != "" {
		return p
	}
	return "pgproof.yml"
}

// load reads the config, applies flag overrides and builds a Runner + logger.
func load() (*backup.Runner, *config.Config, *slog.Logger, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	if flagLogLevel != "" {
		cfg.LogLevel = flagLogLevel
	}
	if flagLogFormat != "" {
		cfg.LogFormat = flagLogFormat
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	runner, err := backup.NewRunner(cfg, log)
	if err != nil {
		return nil, nil, nil, err
	}
	return runner, cfg, log, nil
}
