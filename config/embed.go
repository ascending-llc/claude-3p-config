// Package config holds the deployment config that ccwb-codex-setup is built
// with. It exists only because //go:embed can't reach outside a package's directory.
package config

import "embed"

// files holds config.json when it exists at build time. The pattern also
// matches the committed config.example.json, so the package builds in a fresh
// clone and in CI, where config.json is absent.
//
//go:embed *.json
var files embed.FS

// JSON returns config/config.json as it was when the program was built, or nil
// if the file didn't exist then.
func JSON() []byte {
	data, _ := files.ReadFile("config.json")

	return data
}
