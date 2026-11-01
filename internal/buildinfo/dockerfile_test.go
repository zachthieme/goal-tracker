package buildinfo_test

import (
	"os"
	"regexp"
	"testing"
)

// The Dockerfile takes the image's release tag as its VERSION build argument
// and links it into buildinfo.Version, keeping -s -w, since a Docker build has
// no checkout for Go to name a version from.
func TestTheDockerfileLinksItsVersionArgument(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read the Dockerfile: %v", err)
	}
	dockerfile := string(raw)
	if !regexp.MustCompile(`(?m)^ARG VERSION$`).MatchString(dockerfile) {
		t.Error("the Dockerfile has no ARG VERSION")
	}
	if !regexp.MustCompile(`-ldflags="-s -w[^"]*-X github\.com/zachthieme/goal-tracker/internal/buildinfo\.Version=\$VERSION`).MatchString(dockerfile) {
		t.Error("the Dockerfile's go build doesn't pass -ldflags \"-s -w … -X github.com/zachthieme/goal-tracker/internal/buildinfo.Version=$VERSION\"")
	}
}
