package cmp

import (
	"io/ioutil"
	"log"
	"os"
	"strings"
)

const (
	versionUnknown = "unknown"
)

var version string

func findVersion() string {
	if version != "" {
		return version
	}

	version = versionUnknown
	f, err := os.Open("/current_version")
	if err != nil {
		log.Printf("W! [CMP] Version file (/current_version) not found, using version %q", version)
		return version
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		log.Print("E! [CMP] Failed to read version file (/current_version)")
		return version
	}

	lines := strings.Split(string(b), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "version: ") {
			version = strings.TrimPrefix(l, "version: ")
			break
		}
	}

	return version
}
