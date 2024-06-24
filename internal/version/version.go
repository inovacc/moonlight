// this file version.go was generated with go generate command

package version

import (
	"fmt"
	"strings"
)

var i *Info

type Info struct {
	Version    string   `json:"version"`
	CommitHash string   `json:"commitHash"`
	Date       string   `json:"date"`
	Features   []string `json:"features,omitempty"`
}

func init() {
	i = &Info{
		Version:    "v0.0.0",
		CommitHash: "1df48a2c9b5aaa36966ea5eb7f0879a90764dfde",
		Date:       "2024-06-09T18:25:01Z",
		Features:   []string{},
	}
}

// G returns the Info struct
func G() *Info {
	return i
}

// AddFeature adds a feature description
func AddFeature(feature string) {
	i.Features = append(i.Features, fmt.Sprintf("+%s", feature))
}

// GetVersionInfo returns the info
func GetVersionInfo() string {
	var sb strings.Builder
	sb.WriteString(i.Version)

	if i.CommitHash != "" {
		sb.WriteString("-")
		sb.WriteString(i.CommitHash)
	}

	if i.Date != "" {
		sb.WriteString("-")
		sb.WriteString(i.Date[:10]) // format date to yyyy-mm-dd
	}

	if len(i.Features) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(i.Features, " "))
	}

	return sb.String()
}
