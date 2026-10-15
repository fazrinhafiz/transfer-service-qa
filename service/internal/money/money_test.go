package money

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSenString(t *testing.T) {
	tests := []struct {
		name string
		in   Sen
		want string
	}{
		{"zero", 0, "RM 0.00"},
		{"one sen", 1, "RM 0.01"},
		{"ten sen", 10, "RM 0.10"},
		{"whole ringgit", 100, "RM 1.00"},
		{"thousands separator", 123456, "RM 1,234.56"},
		{"per-transaction limit", 500000, "RM 5,000.00"},
		{"millions", 123456789, "RM 1,234,567.89"},
		{"negative", -150, "-RM 1.50"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.String())
		})
	}
}

func TestFromRinggit(t *testing.T) {
	assert.Equal(t, Sen(500000), FromRinggit(5000))
	assert.Equal(t, Sen(0), FromRinggit(0))
}

// The reason this package exists: summing ten 10-sen amounts must be exactly RM 1.
func TestSenAdditionIsExact(t *testing.T) {
	var total Sen
	for range 10 {
		total += 10
	}
	assert.Equal(t, FromRinggit(1), total)
}
