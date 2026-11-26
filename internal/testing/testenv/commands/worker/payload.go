package worker

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/blevinn/aginctus/internal/testing/testenv"
	"github.com/spf13/cobra"
)

func newPayload() *cobra.Command {
	root := &cobra.Command{Use: "payload", Short: "Inspect staged payload integrity", RunE: groupRun}
	root.AddCommand(&cobra.Command{
		Use: "verify SHA256 BYTES",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return errors.New("invalid payload segment byte count")
			}
			receipt, err := testenv.VerifyPayloadSegment(cmd.InOrStdin(), args[0], n)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "verify-bundle",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			receipt, err := testenv.VerifyPayloadBundle(cmd.InOrStdin())
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
		},
	})
	root.AddCommand(&cobra.Command{
		Use:  "verify-approved-bundle MANIFEST_SHA256",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			receipt, err := testenv.VerifyApprovedPayloadBundle(cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
		},
	})
	return root
}
