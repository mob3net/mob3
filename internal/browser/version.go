package browser

// buildVersion is the app's build version, set at startup with SetBuildVersion.
// It is used by session-generated pages, the config view, the copy header and
// MCP metadata so they agree with -version.
var buildVersion = "dev"

// SetBuildVersion supplies the build version used by session-generated pages.
func SetBuildVersion(v string) {
	if v == "" {
		v = "dev"
	}
	buildVersion = v
}
