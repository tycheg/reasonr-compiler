// Command genmodel writes a synthetic model for scale measurements
// (docs/plans/0015): go run ./internal/genmodel/cmd/genmodel --lines 1m --seed 1 -o dir.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tycheg/reasonr-compiler/internal/genmodel"
)

func main() {
	lines := flag.String("lines", "100k", "model size in lines: 100k, 1m, 10m")
	seed := flag.Uint64("seed", 1, "seed of the size spread")
	out := flag.String("o", "", "model source directory (created; must be empty)")
	flag.Parse()
	n, err := genmodel.ParseLines(*lines)
	if err == nil && *out == "" {
		err = fmt.Errorf("set -o dir")
	}
	if err == nil {
		if entries, _ := os.ReadDir(*out); len(entries) > 0 {
			err = fmt.Errorf("%s is not empty", *out)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "genmodel:", err)
		os.Exit(2)
	}
	st, err := genmodel.Generate(*out, genmodel.Options{Lines: n, Seed: *seed})
	if err != nil {
		fmt.Fprintln(os.Stderr, "genmodel:", err)
		os.Exit(1)
	}
	data, _ := json.Marshal(st)
	fmt.Println(string(data))
}
