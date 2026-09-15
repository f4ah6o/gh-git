package main

import (
	"context"
	"fmt"
	"os"

	"github.com/f4ah6o/gh-git/internal/app"
)

func main() {
	application := app.New(os.Stdout, os.Stderr)
	if err := application.Run(context.Background(), os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "gh git: %s\n", err)
		os.Exit(1)
	}
}
