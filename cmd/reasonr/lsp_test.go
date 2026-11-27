package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func frame(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func TestLSP(t *testing.T) {
	in := frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null}}`) +
		frame(`{"jsonrpc":"2.0","method":"initialized","params":{}}`) +
		frame(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`) +
		frame(`{"jsonrpc":"2.0","method":"exit"}`)
	old := stdin
	stdin = strings.NewReader(in)
	defer func() { stdin = old }()

	var out, errOut bytes.Buffer
	if code := run([]string{"lsp"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d; stderr: %s", code, errOut.String())
	}
	for _, want := range []string{`"id":1`, `"documentFormattingProvider":true`, `"id":2`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in the output:\n%s", want, out.String())
		}
	}

	// exit without shutdown gives code 1.
	stdin = strings.NewReader(frame(`{"jsonrpc":"2.0","method":"exit"}`))
	if code := run([]string{"lsp"}, &out, &errOut); code != 1 {
		t.Errorf("exit without shutdown: code %d", code)
	}
}
