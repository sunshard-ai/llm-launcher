package deploy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeImageCacheAndDownloads(t *testing.T) {
	for _, test := range []struct {
		name, ref, downloaded, pullError, wantError string
		cached                                      bool
	}{
		{name: "cached tag", ref: testPlan().Image, cached: true},
		{name: "cached digest with broken credentials", ref: "ollama/ollama@sha256:" + strings.Repeat("a", 64), cached: true, pullError: "error getting credentials"},
		{name: "missing image", ref: testPlan().Image, downloaded: `[{"Id":"sha256:downloaded"}]`},
		{name: "download failure", ref: testPlan().Image, pullError: "registry unavailable", wantError: "runtime image download"},
		{name: "credential failure", ref: testPlan().Image, pullError: "error getting credentials - err: exit status 1", wantError: "Docker credential helper failed"},
		{name: "download without resolvable image", ref: testPlan().Image, downloaded: `[]`, wantError: "resolve runtime image ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := testPlan()
			p.Image = test.ref
			inspections, pulls := 0, 0
			m := Manager{Dir: t.TempDir()}
			m.runCommand = func(_ context.Context, args []string, _ bool) (string, error) {
				if reflect.DeepEqual(args, []string{"image", "inspect", test.ref}) {
					inspections++
					if test.cached {
						return `[{"Id":"sha256:cached"}]`, nil
					}
					if pulls > 0 {
						return test.downloaded, nil
					}
					return "", errors.New("No such image: " + test.ref)
				}
				if reflect.DeepEqual(args, m.composeArgs(p, "pull", "--policy", "missing")) {
					pulls++
					if test.pullError != "" {
						return "", errors.New(test.pullError)
					}
					return "", nil
				}
				t.Fatalf("unexpected Docker command: %v", args)
				return "", nil
			}
			image, err := m.ensureRuntimeImage(context.Background(), p)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || image.ID != "" {
					t.Fatalf("image=%+v, error=%v", image, err)
				}
			} else if err != nil || image.ID == "" {
				t.Fatalf("image=%+v, error=%v", image, err)
			}
			if test.cached && (pulls != 0 || inspections != 1) {
				t.Fatalf("cached image consulted the registry: inspections=%d, pulls=%d", inspections, pulls)
			}
			if !test.cached && pulls != 1 {
				t.Fatalf("missing image was not downloaded once: %d", pulls)
			}
		})
	}
}

func TestRuntimeImageCancellationPreventsDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := Manager{runCommand: func(_ context.Context, args []string, _ bool) (string, error) {
		if len(args) == 0 || args[0] != "image" {
			t.Fatalf("cancelled inspection reached a download: %v", args)
		}
		return "", ctx.Err()
	}}
	if _, err := m.ensureRuntimeImage(ctx, testPlan()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}
