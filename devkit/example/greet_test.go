package example

import "testing"

func TestGreet(t *testing.T) {
	if Greet() != "hi from friday" {
		t.Fatalf("unexpected greeting: %q", Greet())
	}
}
