package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

func registerStatePromotion(state *cobra.Command, out io.Writer, format func() (clioutput.Format, error), project, id *string) {
	var expected string
	cmd := &cobra.Command{
		Use: "promote", Short: "Publish the configured candidate using authenticated compare-and-swap",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := format()
			if err != nil {
				return err
			}
			if f != clioutput.Table && f != clioutput.JSON {
				return errors.New("state promote supports table or json output")
			}
			if _, err := incusscope.NewProjectScope(*project); err != nil || !client.IsEnvironmentUUID(*id) {
				return errors.New("state promote requires an explicit nondefault Incus project and immutable environment UUID")
			}
			path := os.Getenv("AGINCTUS_RESTRICTED_READ_CONFIG")
			if path == "" {
				return errors.New("state promote requires AGINCTUS_RESTRICTED_READ_CONFIG")
			}
			revision, err := environment.PromoteConfiguredCandidate(cmd.Context(), path, *project, *id, expected)
			if err != nil {
				return err
			}
			if f != clioutput.Table {
				return json.NewEncoder(out).Encode(revision)
			}
			_, err = fmt.Fprintf(out, "Accepted revision: %s (%s/%s, generation %d)\n", revision.ID, revision.Project, revision.EnvironmentID, revision.Generation)
			return err
		},
	}
	cmd.Flags().StringVar(&expected, "expect", "", "Expected accepted revision ID, or genesis if none exists")
	_ = cmd.MarkFlagRequired("expect")
	state.AddCommand(cmd)
}
