package main

import (
	"context"
	"fmt"
	"os"

	"github.com/f4ah6o/gh-git/internal/app"
)

var version = "devel"

func main() {
	application := app.New(os.Stdout, os.Stderr)
	application.Version = version
	if err := application.Run(context.Background(), os.Args[1:], os.Stdin); err != nil {
		if code, ok := app.ExitCode(err); ok {
			os.Exit(code)
		}
		fmt.Fprintf(os.Stderr, "gh git: %s\n", err)
		os.Exit(1)
	}
}
