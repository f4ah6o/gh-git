package gitconfig

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var scpRemotePattern = regexp.MustCompile(`^[^@/:]+@([^/:]+):.+$`)

type Remote struct {
	Name   string
	URL    string
	Scheme string
	Host   string
	SSH    bool
}

func ParseRemote(name, raw string) (Remote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Remote{}, fmt.Errorf("remote %q has an empty URL", name)
	}

	if match := scpRemotePattern.FindStringSubmatch(raw); match != nil {
		return Remote{Name: name, URL: raw, Scheme: "ssh", Host: strings.ToLower(match[1]), SSH: true}, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return Remote{}, fmt.Errorf("remote %q has an unsupported URL %q", name, raw)
	}

	scheme := strings.ToLower(parsed.Scheme)
	return Remote{
		Name:   name,
		URL:    raw,
		Scheme: scheme,
		Host:   strings.ToLower(parsed.Hostname()),
		SSH:    scheme == "ssh",
	}, nil
}

func (r Remote) HTTPS() bool {
	return strings.EqualFold(r.Scheme, "https")
}
