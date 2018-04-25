package cmp

import (
	"fmt"
	"io/ioutil"
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

	f, err := os.Open("/current_version")
	if err != nil {
		fmt.Println("Version file (/current_version) not found")
		version = versionUnknown
		return version
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Println("Failed to read version file (/current_version)")
		version = versionUnknown
		return version
	}

	lines := strings.Split(string(b), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "version: ") {
			version = strings.TrimPrefix(l, "version: ")
			return version
		}
	}

	version = versionUnknown
	return version
}
