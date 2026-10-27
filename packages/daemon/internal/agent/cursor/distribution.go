package cursor

import "github.com/oblien/mindwire/packages/daemon/internal/toolchain"

const version = "2026.10.22-27dcf5f"

func distribution() toolchain.Spec {
	platforms := map[string]toolchain.Archive{}
	for _, os := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"arm64", "amd64"} {
			vendorArch := arch
			if arch == "amd64" {
				vendorArch = "x64"
			}
			ext, binary := "tar.gz", "cursor-agent"
			if os == "windows" {
				ext, binary = "zip", "cursor-agent.cmd"
			}
			archive := toolchain.Archive{URL: "https://downloads.cursor.com/lab/" + version + "/" + os + "/" + vendorArch + "/agent-cli-package." + ext, Executable: "dist-package/" + binary}
			if os == "darwin" && arch == "arm64" {
				archive.SHA256 = "2f5b0a3dd5fb951cecffac813867d8a08d810e59837d59fd5f12d69b922398d7"
			}
			platforms[os+"/"+arch] = archive
		}
	}
	return toolchain.Spec{ID: "cursor", Name: "Cursor", Binary: "cursor-agent", VersionArgs: []string{"--version"}, Archives: map[string]map[string]toolchain.Archive{version: platforms}}
}
