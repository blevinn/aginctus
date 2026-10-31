package compose

import (
	"encoding/json"
	"os"
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

// The actual pinned library must consume the project-local aliases emitted by
// the staging helper. This fake-daemon contract is not packet-level evidence.
func TestTestOCIPinsResolveProjectLocalImages(t *testing.T) {
	body, err := os.ReadFile("../../../../tests/oci-stage/pins.json")
	if err != nil {
		t.Fatal(err)
	}
	var pins struct {
		Images []struct {
			Repository string `json:"repository"`
			Platforms  map[string]struct {
				Manifest    string `json:"manifest"`
				Fingerprint string `json:"incusFingerprint"`
			} `json:"platforms"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &pins); err != nil {
		t.Fatal(err)
	}
	f := newHermeticFixture(t)
	defer f.server.Close()
	f.seedOwnedProject(t, "aginctus-gateway")
	g := composeclient.New(t.Context(), composeclient.ClientProvideConnection(f.conn))
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	c, err := g.EnsureProject("aginctus-gateway")
	if err != nil {
		t.Fatal(err)
	}
	defer c.WarnError(c.Done, "test cleanup failed")
	for _, image := range pins.Images {
		pin := image.Platforms["amd64"]
		ref := image.Repository + "@" + pin.Manifest
		f.server.PutImageAlias("aginctus-gateway", composeclient.ImageAlias(ref), pin.Fingerprint)
		r, err := c.Resource(composeclient.KindImage, ref, &composeclient.ImageConfig{Platform: "linux/amd64"})
		if err != nil {
			t.Fatal(err)
		}
		if err := composeclient.RunAction(t.Context(), r, composeclient.ActionEnsure, composeclient.OptionCreate(), composeclient.OptionPullMode(composeclient.PullNever)); err != nil {
			t.Fatal(err)
		}
		resolved, ok := r.(*composeclient.Image)
		if !ok || resolved.State().IncusAlias.Target != pin.Fingerprint {
			t.Fatal("pinned Compose did not consume the staged image")
		}
	}
	for _, call := range f.server.CallsByPath("/1.0/images") {
		if call.Method != "GET" {
			t.Fatalf("staged image resolution attempted mutation: %v", call)
		}
	}
}
