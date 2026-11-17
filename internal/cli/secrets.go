package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/morrieinmaas/athome/internal/secrets"
)

const secretsUsage = "Usage: athome secrets backup|restore [--dry-run]\n\n  backup           mirror the local secret files into the encrypted vault and push\n  restore          clone the vault, unlock it and copy the files into place\n  restore --dry-run  show what would be written, write nothing\n"

func runSecrets(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, secretsUsage)
		return exitOK
	}
	var restore, dry bool
	switch {
	case len(args) == 1 && args[0] == "backup":
	case len(args) == 1 && args[0] == "restore":
		restore = true
	case len(args) == 2 && args[0] == "restore" && args[1] == "--dry-run":
		restore, dry = true, true
	default:
		fmt.Fprint(stderr, secretsUsage)
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome secrets: %v\n", err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !restore {
		return secrets.Backup(ctx, secrets.BackupFromEnv(home, newRunner(), stdout, stderr))
	}
	o := secrets.FromEnv(home, newRunner(), stdout, stderr)
	o.DryRun = dry
	return secrets.Restore(ctx, o)
}
