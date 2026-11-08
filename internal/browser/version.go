package browser

// Version is the MOB³ release version reported by -version. It is set at
// build time with -ldflags "-X browser/internal/browser.Version=v1.2.3" and
// remains "dev" for local builds that do not pass a version.
var Version = "dev"
