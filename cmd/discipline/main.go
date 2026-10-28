//go:build linux

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/okuuva/discipline/internal/discipline"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "discipline:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("discipline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	state := flags.String("state", "/var/lib/discipline", "private local state directory")
	spool := flags.String("spool", "/srv/abcde", "private local attempt directory")
	drive := flags.String("drive", "/dev/sr0", "one configured optical drive, for serve")
	credential := ""
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		credential = filepath.Join(dir, "telegram-token")
	}
	token := flags.String("telegram-token-file", credential, "private bot token file; defaults to the systemd credential")
	chat := flags.Int64("telegram-chat", 0, "allowed Telegram chat ID, for serve")
	var users []int64
	flags.Func("telegram-user", "allowed Telegram user ID; repeat for each user", func(value string) error {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return errors.New("telegram-user must be a positive numeric ID")
		}
		users = append(users, id)
		return nil
	})
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: discipline [--state DIR --spool DIR] serve|jobs|show UUID|requests|retry UUID|cleanup UUID|new-insertion|reconcile UUID --outcome OUTCOME --note REASON --confirm")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = flags.Args()
	if len(args) == 0 {
		flags.Usage()
		return errors.New("a command is required")
	}
	if args[0] == "serve" {
		if len(args) != 1 {
			return errors.New("unexpected serve arguments")
		}
		return discipline.Serve(ctx, discipline.ServiceConfig{State: *state, Spool: *spool, Drive: *drive, ABCDEConfig: os.Getenv("DISCIPLINE_ABCDE_CONFIG"), Telegram: discipline.TelegramConfig{TokenFile: *token, ChatID: *chat, Users: users}})
	}
	var request discipline.Request
	switch args[0] {
	case "jobs", "requests", "new-insertion":
		if len(args) != 1 {
			return errors.New("unexpected arguments")
		}
		request.Action = args[0]
	case "show", "retry", "cleanup":
		if len(args) != 2 {
			return errors.New("exactly one job UUID is required")
		}
		request.Action, request.JobID = args[0], args[1]
	case "reconcile":
		if len(args) < 2 {
			return errors.New("a job UUID is required")
		}
		request.Action, request.JobID = args[0], args[1]
		reconcile := flag.NewFlagSet("reconcile", flag.ContinueOnError)
		reconcile.SetOutput(stderr)
		reconcile.StringVar(&request.Outcome, "outcome", "", "published, not-published or abandoned")
		reconcile.StringVar(&request.Note, "note", "", "reason and evidence for this decision")
		confirm := reconcile.Bool("confirm", false, "confirm workers/importer are quiesced and outcome was checked")
		if err := reconcile.Parse(args[2:]); err != nil {
			return err
		}
		if !*confirm || request.Note == "" || reconcile.NArg() != 0 {
			return errors.New("reconcile requires --outcome, --note and --confirm; quiesce workers/importer first")
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	store, err := discipline.OpenStore(*state, *spool, false)
	if err != nil {
		return err
	}
	defer store.Close()
	var value any
	switch args[0] {
	case "jobs":
		value, err = store.Jobs(ctx)
	case "show":
		var j discipline.Job
		j, err = store.Job(ctx, args[1])
		if err != nil {
			return err
		}
		var review *discipline.Review
		r, reviewErr := store.Review(ctx, j.ID)
		if reviewErr == nil {
			review = &r
		} else if !errors.Is(reviewErr, sql.ErrNoRows) {
			return reviewErr
		}
		value = struct {
			Job       discipline.Job     `json:"job"`
			Review    *discipline.Review `json:"review,omitempty"`
			LocalPath string             `json:"local_path"`
		}{j, review, filepath.Join(*spool, j.ID)}
	case "requests":
		value, err = store.Requests(ctx)
	default:
		value, err = store.Enqueue(ctx, request)
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
