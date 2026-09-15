package gitconfig

import "testing"

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		scheme string
		host   string
		ssh    bool
	}{
		{name: "https", input: "https://github.com/example/gh-git.git", scheme: "https", host: "github.com"},
		{name: "scp", input: "git@github.com:example/gh-git.git", scheme: "ssh", host: "github.com", ssh: true},
		{name: "ssh url", input: "ssh://git@github.com/example/gh-git.git", scheme: "ssh", host: "github.com", ssh: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemote("origin", tt.input)
			if err != nil {
				t.Fatalf("ParseRemote() error = %v", err)
			}
			if got.Scheme != tt.scheme || got.Host != tt.host || got.SSH != tt.ssh {
				t.Fatalf("ParseRemote() = %+v", got)
			}
		})
	}
}
