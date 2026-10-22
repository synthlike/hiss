// Command hiss is a modal terminal editor for reviewing AI agents' edits.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v3"
	"github.com/synthlike/hiss/internal/core"
	"github.com/synthlike/hiss/internal/ui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hiss:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: hiss <file>")
	}
	buf, err := core.Open(args[0])
	if err != nil {
		return err
	}
	scr, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := scr.Init(); err != nil {
		return err
	}
	defer scr.Fini()
	ui.Run(core.New(buf), scr, nil)
	return nil
}
