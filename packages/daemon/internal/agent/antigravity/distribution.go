package antigravity

import (
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
	"strings"
)

const version = "1.4.3"

func distribution() toolchain.Spec {
	hashes := map[string]string{
		"darwin/arm64":  "c57db0a9b8b712daa9c3c17cdb990a61842ea569f279e8bd79d7aa85a303b20b",
		"darwin/amd64":  "9392dbbdad7f0663dbfb5a0bf641297b0cf80d2ee9cd2d49a616bbbc7378731d",
		"linux/amd64":   "0634ce5306ba92c932945455e7a83fda78e2d99c33fa8aa6ee0566206a382714",
		"linux/arm64":   "05c153443b0b1175151c760eeddea538950bc9843e317d54786a1f03670182bc",
		"windows/amd64": "54c2e16378d9a4952e1da70e355f3c33c4f284308d6de48ab3e6e34a127bd325",
		"windows/arm64": "fcd46431ad33d3b510ce0b98fe7b43d095bd8b3f1c363ee2d57faa7ee981c7bb",
	}
	platforms := map[string]toolchain.Archive{}
	for platform, hash := range hashes {
		os, arch, _ := strings.Cut(platform, "/")
		vendorArch := arch
		if arch == "amd64" {
			vendorArch = "x86_64"
		}
		dir, binary, helper := os, "agy_acp_server.par", "localharness_external"
		if os == "darwin" {
			dir = "macos"
		}
		if os == "windows" {
			binary, helper = "agy_acp_server.exe", "localharness_external.exe"
		}
		platforms[platform] = toolchain.Archive{URL: "https://dl.google.com/agy-extensions/releases/" + dir + "/agy-acp-server-" + version + "-" + os + "-" + vendorArch + ".zip", SHA256: hash, Executable: binary, Executables: []string{helper}, EnvironmentPaths: map[string]string{"ANTIGRAVITY_HARNESS_PATH": helper}}
	}
	return toolchain.Spec{ID: "antigravity", Name: "Antigravity", Binary: "agy-acp", VersionArgs: []string{"--version"}, VersionPattern: `(?m)^Build label: ([0-9]+\.[0-9]+\.[0-9]+)`, Archives: map[string]map[string]toolchain.Archive{version: platforms}}
}
