package sample

import (
	"errors"
	"os"
	"sync"
)
// Config holds service settings from the environment.
type Config struct{ Addr, Port string }
// LoadConfig reads settings from the environment.
func LoadConfig() Config {
	return Config{os.Getenv("SAMPLE_ADDR"), os.Getenv("SAMPLE_PORT")}
}
// Greet greets a name or returns an error.
func Greet(name string) (string, error) {
	if name == "" {
		return "", errors.New("greet: empty name")
	}
	return "hello " + name, nil
}
// Fanout greets names concurrently then waits.
func Fanout(names []string) []string {
	var wg sync.WaitGroup
	out := make([]string, len(names))
	for i, n := range names {
		wg.Add(1)
		go func() { defer wg.Done(); s, _ := Greet(n); out[i] = s }()
	}
	wg.Wait()
	return out
}
// TestGreet exercises Greet with a fixed name.
func TestGreet(t interface{ Errorf(string, ...any) }) {
	if _, err := Greet("ada"); err != nil {
		t.Errorf("greet: %v", err)
	}
}
