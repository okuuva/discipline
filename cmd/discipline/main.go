package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/okuuva/discipline/internal/discipline"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "discipline:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("discipline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	state := flags.String("state", "/var/lib/discipline", "private local state directory")
	spool := flags.String("spool", "/srv/abcde", "private local attempt directory")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: discipline [--state DIR --spool DIR] jobs|show UUID|requests|retry UUID|cleanup UUID|new-insertion|reconcile UUID --outcome OUTCOME --note REASON --confirm")
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
		value, err = store.Job(ctx, args[1])
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
