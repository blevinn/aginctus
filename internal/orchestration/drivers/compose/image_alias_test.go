package compose

import (
	"testing"

	composeclient "github.com/lxc/incus-compose/client"
)

func TestImmutableImageAliasMatchesDeliveryAudit(t *testing.T) {
	const digest = "sha256:bd089afdcd35b894b14a93f9743cdc8b591f82da1a38dd43a010a7b0c9de5fd7"
	const expected = "ic-image-eb2dh5p4ong4xc43eddosshm2xl4nntvmxxub2ksgsmeqtjkyzwa"
	c := composeclient.NewOfflineClient(t.Context(), "test")
	for _, ref := range []string{
		"ghcr.io/berriai/litellm:v1.103.0@" + digest,
		"ghcr.io/berriai/litellm@" + digest,
	} {
		r, err := c.Resource(composeclient.KindImage, ref, &composeclient.ImageConfig{})
		if err != nil {
			t.Fatal(err)
		}
		image, ok := r.(*composeclient.Image)
		if !ok || image.IncusName() != expected || len(image.IncusName()) > 64 {
			t.Fatalf("immutable image has an incompatible Incus alias")
		}
	}
}
