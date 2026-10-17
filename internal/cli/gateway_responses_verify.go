package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/responsescontract"
)

// The validator reads the stream once from stdin. Tests substitute a synthetic
// reader; neither raw event bodies nor upstream error details are echoed.
var gatewayResponsesInput io.Reader = os.Stdin

func verifyGatewayResponsesStream(args []string, stdout, stderr io.Writer) int {
	requireFunctionCall := false
	if len(args) > 1 || (len(args) == 1 && args[0] != "--require-function-call") {
		fmt.Fprintln(stderr, "Usage: aginctus gateway responses verify [--require-function-call] < stream.sse")
		return 2
	}
	if len(args) == 1 {
		requireFunctionCall = true
	}
	summary, err := responsescontract.Verify(gatewayResponsesInput, requireFunctionCall)
	if err != nil {
		fmt.Fprintf(stderr, "Responses stream contract: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Responses stream: completed; %d events, %d completed function calls (offline transcript validation only)\n",
		summary.Events, summary.FunctionCalls)
	return 0
}
