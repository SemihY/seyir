package version

import (
	"fmt"
	"runtime"
	"strings"
)

// Build-time variables set by ldflags
var (
	// Version is the current version of the application
	Version = "dev"
	
	// Commit is the git commit hash
	Commit = "unknown"
	
	// BuildDate is when the binary was built
	BuildDate = "unknown"
	
	// GoVersion is the Go version used to build
	GoVersion = runtime.Version()
)

// Info holds version information
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns version information
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: GoVersion,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// String returns a formatted version string
func (i Info) String() string {
	var parts []string
	
	parts = append(parts, fmt.Sprintf("Version: %s", i.Version))
	
	if i.Commit != "unknown" {
		commit := i.Commit
		if len(commit) > 8 {
			commit = commit[:8]
		}
		parts = append(parts, fmt.Sprintf("Commit: %s", commit))
	}
	
	if i.BuildDate != "unknown" {
		parts = append(parts, fmt.Sprintf("Built: %s", i.BuildDate))
	}
	
	parts = append(parts, fmt.Sprintf("Go: %s", i.GoVersion))
	parts = append(parts, fmt.Sprintf("Platform: %s/%s", i.OS, i.Arch))
	
	return strings.Join(parts, "\n")
}

// Short returns a short version string suitable for logging
func (i Info) Short() string {
	if i.Commit != "unknown" && len(i.Commit) > 8 {
		return fmt.Sprintf("%s (%s)", i.Version, i.Commit[:8])
	}
	return i.Version
}

// GetVersion returns the current version string
func GetVersion() string {
	return Version
}

// GetCommit returns the git commit hash
func GetCommit() string {
	return Commit
}

// GetBuildDate returns the build date
func GetBuildDate() string {
	return BuildDate
}

// IsDevBuild returns true if this is a development build
func IsDevBuild() bool {
	return Version == "dev" || strings.Contains(Version, "dev")
}