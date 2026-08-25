package utils_test

import (
	"testing"

	"github.com/godopetza/pitchtz/utils"
)

func TestNormalizeTZPhone(t *testing.T) {
	tests := map[string]string{
		"0712 345 678":     "+255712345678",
		"+255 712-345-678": "+255712345678",
		"255712345678":     "+255712345678",
		"712345678":        "+255712345678",
	}
	for input, expected := range tests {
		actual, err := utils.NormalizeTZPhone(input)
		if err != nil || actual != expected {
			t.Fatalf("NormalizeTZPhone(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	for _, input := range []string{"", "123", "+1 555 123 4567", "0812 345 678", "call 0712 345 678"} {
		if _, err := utils.NormalizeTZPhone(input); err == nil {
			t.Fatalf("NormalizeTZPhone(%q) should fail", input)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	actual, err := utils.NormalizeEmail(" Player@Example.COM ")
	if err != nil || actual != "player@example.com" {
		t.Fatalf("unexpected normalization: %q, %v", actual, err)
	}
	if _, err := utils.NormalizeEmail("not-an-email"); err == nil {
		t.Fatal("invalid email should fail")
	}
}
