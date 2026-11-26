package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

func runProjectStateAttachment(args []string) int {
	var target environment.ProjectServiceAttachment
	flags := flag.NewFlagSet("aginctus internal project-state-attachment", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: aginctus internal project-state-attachment --server-id ID --project NAME --instance NAME --pool NAME --volume NAME --device NAME --mount-path PATH [--network NAME]")
		flags.PrintDefaults()
	}
	flags.StringVar(&target.Binding.ServerID, "server-id", "", "Independently pinned Incus server ID (sha256:<certificate fingerprint>)")
	flags.StringVar(&target.Binding.Project, "project", "", "Exact restricted Incus project")
	flags.StringVar(&target.Instance, "instance", "", "Dedicated state-service container name")
	flags.StringVar(&target.Pool, "pool", "", "State volume pool")
	flags.StringVar(&target.Volume, "volume", "", "Project-owned custom filesystem volume")
	flags.StringVar(&target.Device, "device", "", "Explicit state volume device name")
	flags.StringVar(&target.MountPath, "mount-path", "", "Exact in-guest mount path")
	flags.StringVar(&target.Network, "network", "", "Exact managed network for eth0; omit for no NIC")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || target.Binding.ServerID == "" || target.Binding.Project == "" || target.Instance == "" ||
		target.Pool == "" || target.Volume == "" || target.Device == "" || target.MountPath == "" {
		flags.Usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := environment.ObserveLocalProjectServiceAttachment(ctx, target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "project-state attachment verified")
	return 0
}
