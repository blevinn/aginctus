package imageops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ListLocalImages renders the ambient v0.1 image list without changing its
// command dispatch or authorizing image mutation.
func ListLocalImages(ctx context.Context, stdout, stderr io.Writer, run func(context.Context, ...string) ([]byte, error)) int {
	output, err := run(ctx, "image", "list", "--format", "json")
	if err != nil {
		fmt.Fprintf(stderr, "list images: %v\n", err)
		return 1
	}
	var images []LocalImage
	if err := json.Unmarshal(output, &images); err != nil {
		fmt.Fprintf(stderr, "parse Incus image list: %v\n", err)
		return 1
	}
	PrintLocalImages(stdout, images)
	return 0
}

func LocalImages(ctx context.Context, run func(context.Context, ...string) ([]byte, error)) ([]LocalImage, error) {
	out, err := run(ctx, "image", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var images []LocalImage
	if err := json.Unmarshal(out, &images); err != nil {
		return nil, fmt.Errorf("parse Incus image list: %w", err)
	}
	return images, nil
}
