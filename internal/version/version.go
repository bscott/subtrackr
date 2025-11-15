package version

var (
	// GitCommit is the git commit SHA that will be set at build time
	GitCommit = "unknown"
	// Version is the semantic version tag that will be set at build time
	Version = "dev"
)

// GetVersion returns the current version string
// Prefers the semantic version tag over git commit SHA
func GetVersion() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	if GitCommit != "unknown" && GitCommit != "" {
		return GitCommit
	}
	return "dev"
}