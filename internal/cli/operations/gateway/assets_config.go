package gatewayops

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

// ReadAssetConfig reads a bounded, nonsecret JSON gateway asset declaration.
// File access is a CLI-injected operation, never a Cobra registration effect.
// Preserve O_NOFOLLOW, nonblocking open and regular-file checks; do not echo
// raw paths or OS errors that could contain sensitive host information.
func ReadAssetConfig(inline, file string, result any) error {
	data := []byte(inline)
	if file != "" {
		input, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("configuration file unavailable")
		}
		defer input.Close()
		info, err := input.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("configuration must be a regular file")
		}
		data, err = io.ReadAll(io.LimitReader(input, 1<<20+1))
		if err != nil {
			return fmt.Errorf("configuration file unavailable")
		}
	}
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("configuration exceeds size limit")
	}
	return gatewaycontrol.Decode(data, result)
}
