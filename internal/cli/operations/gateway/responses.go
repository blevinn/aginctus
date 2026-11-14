package gatewayops

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/responsescontract"
)

// VerifyResponsesStream consumes a caller-provided offline SSE transcript only;
// no gateway runtime, network, credentials, or Incus operation is performed.
func VerifyResponsesStream(args []string, stdout, stderr io.Writer, input io.Reader) int {
	requireFunctionCall := false
	if len(args) > 1 || (len(args) == 1 && args[0] != "--require-function-call") {
		fmt.Fprintln(stderr, "Usage: aginctus gateway responses verify [--require-function-call] < stream.sse")
		return 2
	}
	if len(args) == 1 {
		requireFunctionCall = true
	}
	summary, err := responsescontract.Verify(input, requireFunctionCall)
	if err != nil {
		fmt.Fprintf(stderr, "Responses stream contract: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Responses stream: completed; %d events, %d completed function calls (offline transcript validation only)\n",
		summary.Events, summary.FunctionCalls)
	return 0
}
