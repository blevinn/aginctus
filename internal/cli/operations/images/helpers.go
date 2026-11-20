package imageops

import (
	"fmt"
	"io"

	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// LocalImage and PrintLocalImages preserve the v0.1 human-readable CLI
// inventory contract; no project is inferred by the renderer.
type LocalImage = incusimages.LocalImage

func PrintLocalImages(stdout io.Writer, images []LocalImage) {
	for _, img := range images {
		if len(img.Aliases) == 0 {
			fmt.Fprintln(stdout, img.Fingerprint)
			continue
		}
		for _, a := range img.Aliases {
			fmt.Fprintf(stdout, "%s\t%s\n", a.Name, img.Fingerprint)
		}
	}
}

// Existing CLI callers delegate to resource-owned Nix artifact handling,
// fingerprinting and collision-safe temporary alias construction.
func Artifact(dir, subdir, extension string) (string, error) {
	return incusimages.Artifact(dir, subdir, extension)
}

func ValidImageName(name string) bool { return incusimages.ValidImageName(name) }

func Fingerprint(metadata, rootfs string) (string, error) {
	return incusimages.Fingerprint(metadata, rootfs)
}

func TemporaryImageAlias(name, role, nonce string) string {
	return incusimages.TemporaryImageAlias(name, role, nonce)
}
