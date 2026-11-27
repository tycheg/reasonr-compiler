//go:build race

package vm

// raceEnabled: under the race detector sync.Pool loses objects, and the
// allocation check does not apply.
const raceEnabled = true
