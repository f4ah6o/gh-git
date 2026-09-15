package sshconfig

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HasHostAlias checks only an existing, exact Host block. It never creates or
// modifies SSH configuration and deliberately ignores wildcard/negated blocks.
func HasHostAlias(alias, expectedHost string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	file, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	return hasHostAlias(file, alias, expectedHost)
}

func hasHostAlias(reader io.Reader, alias, expectedHost string) (bool, error) {
	active := false
	hostName := ""
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			if active && strings.EqualFold(hostName, expectedHost) {
				return true, nil
			}
			active = false
			hostName = ""
			for _, pattern := range fields[1:] {
				if pattern == alias && !strings.ContainsAny(pattern, "*!?") {
					active = true
				}
			}
		case "hostname":
			if active && len(fields) >= 2 {
				hostName = fields[1]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return active && strings.EqualFold(hostName, expectedHost), nil
}
