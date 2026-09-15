package shell

import (
	"strings"
	"testing"
)

func TestInitContainsProfileSelection(t *testing.T) {
	for _, name := range []string{"bash", "zsh", "fish"} {
		output, err := Init(name)
		if err != nil {
			t.Fatalf("Init(%q): %v", name, err)
		}
		if !strings.Contains(output, ".gh-git-profile") || !strings.Contains(output, "GH_CONFIG_DIR") {
			t.Fatalf("Init(%q) does not select the generated profile", name)
		}
	}
}
