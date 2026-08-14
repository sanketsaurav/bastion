// Package version records the CLI build version.
package version

// Version is the CLI version. Release builds override it via
// -ldflags "-X github.com/sanketsaurav/bastion/internal/version.Version=vX.Y.Z".
var Version = "0.1.0-dev"
