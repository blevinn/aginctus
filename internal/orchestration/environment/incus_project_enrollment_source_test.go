package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestIncusProjectEnrollmentSourceComposesWithRestrictedVerifier(t *testing.T) {
	verifier, observer := restrictedIncusVerifierFixture(t)
	binding := Binding{ServerID: "sha256:" + verifier.PinnedServerCertificateSHA256, Project: "team"}
	observer.selected.Config = map[string]string{
		incusEnvironmentEnrollmentKey: `{"apiVersion":"aginctus.io/environment-enrollment/v1alpha1","items":[{"id":"22222222-2222-4222-8222-222222222222","name":"dev","binding":{"serverId":"` + binding.ServerID + `","project":"team"}},{"id":"11111111-1111-4111-8111-111111111111","name":"dev","binding":{"serverId":"` + binding.ServerID + `","project":"team"}}]}`,
	}
	catalog := NewRestrictedIncusReadCatalog(observer, verifier.PinnedServerCertificateSHA256)
	items, err := catalog.List(context.Background(), "team")
	if err != nil || len(items) != 2 || items[0].ID != firstID || items[1].ID != secondID {
		t.Fatalf("project enrollment=%+v err=%v", items, err)
	}
	if items[0].Name != "dev" || items[0].Binding != binding {
		t.Fatalf("enrollment lost binding: %+v", items[0])
	}
	if items, err := catalog.List(context.Background(), "foreign"); err == nil || len(items) != 0 {
		t.Fatalf("foreign inventory escaped: %+v err=%v", items, err)
	}
}

func TestIncusProjectEnrollmentSourceRejectsBrokenManifest(t *testing.T) {
	verifier, observer := restrictedIncusVerifierFixture(t)
	binding := Binding{ServerID: "sha256:" + verifier.PinnedServerCertificateSHA256, Project: "team"}
	good := `{"apiVersion":"aginctus.io/environment-enrollment/v1alpha1","items":[{"id":"11111111-1111-4111-8111-111111111111","name":"dev","binding":{"serverId":"` + binding.ServerID + `","project":"team"}}]}`
	src := IncusProjectEnrollmentSource{Observer: observer}
	for _, tc := range []struct{name, data string}{
		{"missing", ""},
		{"missing items", `{"apiVersion":"aginctus.io/environment-enrollment/v1alpha1"}`},
		{"unsupported version", strings.Replace(good, "v1alpha1", "v2", 1)},
		{"foreign project", strings.Replace(good, `"project":"team"`, `"project":"other"`, 1)},
		{"foreign server", strings.Replace(good, binding.ServerID, "untrusted-server", 1)},
		{"duplicate root key", strings.Replace(good, `"apiVersion":`, `"apiVersion":"duplicate","apiVersion":`, 1)},
		{"unknown property", strings.Replace(good, `"items":`, `"unexpected":true,"items":`, 1)},
		{"duplicate descriptor", strings.Replace(good, `]}`, `,{"id":"11111111-1111-4111-8111-111111111111","name":"dev","binding":{"serverId":"` + binding.ServerID + `","project":"team"}}]}`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer.selected.Config = map[string]string{incusEnvironmentEnrollmentKey: tc.data}
			result, err := (Catalog{Verifier: verifier, Source: src}).List(context.Background(), "team")
			if err == nil || result != nil {
				t.Fatalf("invalid enrollment escaped: %+v err=%v", result, err)
			}
		})
	}
	observer.selected.Config = map[string]string{incusEnvironmentEnrollmentKey: good}
	src.Observer = nil
	if result, err := src.Enrolled(context.Background(), binding); err == nil || result != nil {
		t.Fatalf("missing physical source accepted: %+v %v", result, err)
	}
}

type changingEnrollmentProject struct {
	*fixtureRestrictedIncusObserver
	projectCalls int
}

func (r *changingEnrollmentProject) GetProject(name string) (*api.Project, string, error) {
	if name == "default" {
		return nil, "", api.StatusErrorf(403, "not authorized")
	}
	r.projectCalls++
	if r.projectCalls == 1 {
		return r.selected, "etag-1", nil
	}
	return r.selected, "etag-2", nil
}

func TestIncusProjectEnrollmentSourceRejectsChangedETag(t *testing.T) {
	verifier, base := restrictedIncusVerifierFixture(t)
	binding := Binding{ServerID: "sha256:" + verifier.PinnedServerCertificateSHA256, Project: "team"}
	base.selected.Config = map[string]string{
		incusEnvironmentEnrollmentKey: `{"apiVersion":"aginctus.io/environment-enrollment/v1alpha1","items":[]}`,
	}
	changing := &changingEnrollmentProject{fixtureRestrictedIncusObserver: base}
	out, err := (IncusProjectEnrollmentSource{Observer: changing}).Enrolled(context.Background(), binding)
	if err == nil || out != nil || changing.projectCalls != 2 {
		t.Fatalf("changed enrollment passed: %+v err=%v reads=%d", out, err, changing.projectCalls)
	}
	var noContext context.Context
	if out, err := (IncusProjectEnrollmentSource{Observer: changing}).Enrolled(noContext, binding);
		err == nil || out != nil {
		t.Fatalf("nil context accepted: %+v err=%v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := (IncusProjectEnrollmentSource{Observer: changing}).Enrolled(ctx, binding);
		!errors.Is(err, context.Canceled) || out != nil {
		t.Fatalf("cancellation ignored: %+v err=%v", out, err)
	}
}
