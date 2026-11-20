package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/workload"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
)

// runTypedScopedWorkloadStatus is a presentation adapter over the verified
// domain reader. It never queries the ambient Incus project, and deliberately
// does not claim that an Incus project equals an Aginctus environment.
func runTypedScopedWorkloadStatus(ctx context.Context, project, name string, runningOnly bool, format clioutput.Format, out io.Writer, cfg *config.Config) error {
	reader := workload.ScopedStatusReader{
		Config: cfg,
		Verify: verifyScopedImageProject,
		Inspect: inspectScopedInstances,
		MaxBytes: maxProjectListBytes,
	}
	c := client.New(client.Options{ScopedWorkloadReader: reader})
	result, err := c.Workloads.ScopedStatus(ctx, api.ScopedWorkloadStatusRequest{
		Project: project, Name: name, RunningOnly: runningOnly,
	})
	if err != nil {
		return err
	}
	switch format {
	case clioutput.JSON, clioutput.YAML:
		return clioutput.WriteStructured(out, format, result)
	case clioutput.Name:
		for _, item := range result.Items {
			if _, err := fmt.Fprintln(out, item.Name); err != nil {
				return err
			}
		}
		return nil
	case clioutput.Table, clioutput.Wide:
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if format == clioutput.Wide {
			if _, err := fmt.Fprintln(writer, "NAME\tINSTANCE\tSTATE\tPROJECT"); err != nil {
				return err
			}
			for _, item := range result.Items {
				if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", item.Name, item.Instance, item.State, result.Project); err != nil {
					return err
				}
			}
		} else {
			if _, err := fmt.Fprintln(writer, "NAME\tSTATE"); err != nil {
				return err
			}
			for _, item := range result.Items {
				if _, err := fmt.Fprintf(writer, "%s\t%s\n", item.Name, item.State); err != nil {
					return err
				}
			}
		}
		return writer.Flush()
	default:
		return fmt.Errorf("unsupported scoped workload output %q", strings.TrimSpace(string(format)))
	}
}
