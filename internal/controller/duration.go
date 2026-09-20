package controller

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var leaseDurationPart = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)(ms|s|m|h|d|w)`)

var leaseDurationUnits = map[string]time.Duration{
	"ms": time.Millisecond,
	"s":  time.Second,
	"m":  time.Minute,
	"h":  time.Hour,
	"d":  24 * time.Hour,
	"w":  7 * 24 * time.Hour,
}

func ParseLeaseDuration(value string) (time.Duration, error) {
	parts := leaseDurationPart.FindAllStringSubmatch(value, -1)
	if len(parts) == 0 {
		return 0, fmt.Errorf("must use one or more ms, s, m, h, d, or w units")
	}

	var parsed strings.Builder
	total := new(big.Rat)
	maximum := new(big.Rat).SetInt64(int64(mathMaxDuration))
	for _, part := range parts {
		parsed.WriteString(part[0])
		amount, ok := new(big.Rat).SetString(part[1])
		if !ok {
			return 0, fmt.Errorf("invalid number %q", part[1])
		}
		term := new(big.Rat).Mul(amount, new(big.Rat).SetInt64(int64(leaseDurationUnits[part[2]])))
		total.Add(total, term)
		if total.Cmp(maximum) > 0 {
			return 0, fmt.Errorf("value is too large")
		}
	}
	if parsed.String() != value {
		return 0, fmt.Errorf("%q must use one or more ms, s, m, h, d, or w units", value)
	}

	nanoseconds := new(big.Int).Quo(total.Num(), total.Denom())
	duration := time.Duration(nanoseconds.Int64())
	if duration <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}
	return duration, nil
}

const mathMaxDuration = time.Duration(1<<63 - 1)

func FormatLeaseDuration(duration time.Duration) string {
	for _, unit := range []struct {
		suffix string
		value  time.Duration
	}{
		{suffix: "w", value: 7 * 24 * time.Hour},
		{suffix: "d", value: 24 * time.Hour},
	} {
		if duration >= unit.value && duration%unit.value == 0 {
			return fmt.Sprintf("%d%s", duration/unit.value, unit.suffix)
		}
	}
	return duration.String()
}
