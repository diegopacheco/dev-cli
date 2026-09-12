package main

import (
	"fmt"
	"os"

	"github.com/diegopacheco/dev-cli/internal/cli"
)

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func main() {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "devcli:", err, "(run devcli --help)")
		os.Exit(2)
	}
	stdio := cli.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, InTTY: isTTY(os.Stdin), OutTTY: isTTY(os.Stdout), ErrTTY: isTTY(os.Stderr)}
	os.Exit(cli.Main(opts, cli.DefaultTargets(), stdio))
}
