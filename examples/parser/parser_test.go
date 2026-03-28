package parser

import "testing"

func TestParseInt(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		got, err := ParseInt(" 42 ")
		if err != nil {
			t.Fatalf("ParseInt returned error: %v", err)
		}
		if got != 42 {
			t.Fatalf("expected 42, got %d", got)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := ParseInt("abc")
		if err == nil {
			t.Fatal("expected parse error")
		}
	})
}
