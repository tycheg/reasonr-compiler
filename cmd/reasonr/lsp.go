package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tycheg/reasonr-compiler/internal/lsp"
)

// stdin is the input of the LSP server; tests replace it.
var stdin io.Reader = os.Stdin

// lspCmd is reasonr lsp: the Language Server Protocol server on stdin and stdout.
func lspCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("lsp", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Bool("stdio", true, "talk over stdin and stdout (the only mode)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if err := lsp.Serve(stdin, stdout, lsp.Options{Log: stderr}); err != nil {
		fmt.Fprintf(stderr, "reasonr lsp: %v\n", err)
		return 1
	}
	return 0
}
