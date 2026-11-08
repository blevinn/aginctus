package main

import (
	"context"
	"os"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
	workercmd "github.com/blevinn/aginctus/internal/testenv/commands/worker"
)

type workerServices struct {
	getenv func(string) string
	preflight func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error)
	accept func(context.Context, testenv.IncusAcceptanceOptions) (testenv.IncusAcceptanceReport, error)
	sweep func(context.Context, testenv.TrustedIncusSweepOptions, time.Time) (testenv.IncusReconcileResult, error)
	now func() time.Time
}

// This build provenance is injected by the single testenv-cli flake package.
var revision = "source-unverified"

func defaultWorkerServices() workerServices {
	return workerServices{
		getenv: os.Getenv,
		preflight: testenv.RunTrustedIncusWorkerPreflight,
		accept: testenv.RunTrustedIncusAcceptance,
		sweep: testenv.RunTrustedIncusExpirySweep,
		now: time.Now,
	}
}

func (s workerServices) handlers() workercmd.Handlers {
	return workercmd.Handlers{
		Getenv: s.getenv,
		Preflight: s.preflight,
		Accept: s.accept,
		Sweep: s.sweep,
		Now: s.now,
		Revision: revision,
	}
}
