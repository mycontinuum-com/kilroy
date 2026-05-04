// Package version holds the Kilroy release version.
//
// Version is the canonical version for all builds (source, binary, Homebrew).
// goreleaser also injects it at build time via ldflags from the git tag.
package version

// Version is the current Kilroy release version.
// This value is bumped as part of the release process (see skills/release-kilroy/SKILL.md).
// goreleaser overrides it at build time: go build -ldflags "-X github.com/danshapiro/kilroy/internal/version.Version=1.2.3"
var Version = "0.1.1"
