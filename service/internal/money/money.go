// Package money represents Malaysian ringgit amounts as integer sen.
//
// Floating point cannot represent most decimal fractions exactly
// (0.1 + 0.2 != 0.3 in float64), which is unacceptable for balances.
// Storing whole sen in an int64 makes every addition and comparison exact.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Sen is an amount of money in sen, where 100 sen = RM 1.
type Sen int64

// FromRinggit converts a whole ringgit amount to sen.
func FromRinggit(rm int64) Sen {
	return Sen(rm * 100)
}

// String formats the amount for display, e.g. "RM 1,234.56".
func (s Sen) String() string {
	sign := ""
	v := int64(s)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%sRM %s.%02d", sign, groupThousands(v/100), v%100)
}

func groupThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}
