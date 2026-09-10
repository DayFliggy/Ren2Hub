package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, model.RoutingMigrationDisplayError(err))
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: ren2hub-migrate inspect|apply|resume|verify [--role main|log] [--sqlite path] [--quiesced]")
	}
	mode := os.Args[1]
	if mode != "inspect" && mode != "apply" && mode != "resume" && mode != "verify" {
		return fmt.Errorf("unknown migration command")
	}
	args := flag.NewFlagSet("ren2hub-migrate", flag.ContinueOnError)
	role := args.String("role", "main", "database role")
	sqlite := args.String("sqlite", "one-api.db", "SQLite path; ignored when a network DSN is configured")
	quiesced := args.Bool("quiesced", false, "all writers are stopped and a complete backup exists")
	if err := args.Parse(os.Args[2:]); err != nil {
		return err
	}
	if (mode == "apply" || mode == "resume") && !*quiesced {
		return fmt.Errorf("apply/resume requires --quiesced after stopping all writers and creating a backup")
	}
	common.SQLitePath = *sqlite
	db, err := model.OpenRoutingMigrationDatabase(*role)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var report model.RoutingMigrationReport
	switch mode {
	case "inspect":
		report, err = model.InspectRoutingMigration(ctx, db, *role)
	case "verify":
		report, err = model.VerifyRoutingMigration(ctx, db, *role)
	case "resume":
		report, err = model.ResumeRoutingMigration(ctx, db, *role)
	default:
		report, err = model.ApplyRoutingMigration(ctx, db, *role)
	}
	data, encodeErr := common.Marshal(report)
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(data))
	return err
}
