package sampler

import (
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
)

// fieldSearchKey is a two-character OpenTelemetry tracestate field name
// (e.g., "rv", "th"), preceded by ';', followed by ':'.
type fieldSearchKey string

const randomnessSearchKey fieldSearchKey = ";rv:"
const thresholdSearchKey fieldSearchKey = ";th:"

func tracestateHasOTelField(otts string, search fieldSearchKey) (value string, has bool) {
	var low int
	if has := strings.HasPrefix(otts, string(search[1:])); has {
		low = 3
	} else if pos := strings.Index(otts, string(search)); pos > 0 {
		low = pos + 4
	} else {
		return "", false
	}
	high := strings.IndexByte(otts[low:], ';')
	if high < 0 {
		high = len(otts)
	}
	return otts[low:high], true
}

// tracestateHasRandomness determines whether there is a "rv" sub-key
func tracestateHasRandomness(otts string) (int64, bool) {
	val, has := tracestateHasOTelField(otts, randomnessSearchKey)
	if !has {
		return 0, false
	}
	if len(val) != 14 {
		otel.Handle(fmt.Errorf("could not parse tracestate randomness: %q: %w", otts, strconv.ErrSyntax))
		return 0, false
	}
	rv, err := strconv.ParseUint(val, 16, 64)
	if err != nil {
		otel.Handle(fmt.Errorf("could not parse tracestate randomness: %q: %w", val, err))
		return 0, false
	}
	return int64(rv), true
}

// tracestateHasThreshold determines whether there is a "th" sub-key
func tracestateHasThreshold(otts string) (int64, bool) {
	val, has := tracestateHasOTelField(otts, thresholdSearchKey)
	if !has {
		return 0, false
	}
	if len(val) == 0 || len(val) > 14 {
		otel.Handle(fmt.Errorf("could not parse tracestate threshold: %q: %w", otts, strconv.ErrSyntax))
		return 0, false
	}
	th, err := strconv.ParseUint(val, 16, 64)
	if err != nil {
		otel.Handle(fmt.Errorf("could not parse tracestate threshold: %q: %w", val, err))
		return 0, false
	}
	// Add trailing zeros
	th <<= (14 - len(val)) * 4
	return int64(th), true
}

// combineTracestate combines an existing OTel tracestate fragment,
// which is the value of a top-level "ot" tracestate vendor tag.
func combineTracestate(incoming, updated string) string {
	// `incoming` is formatted according to the OTel tracestate
	// spec, with colon separating two-byte key and value, with
	// semi-colon separating key-value pairs.
	//
	// `updated` should be a single two-byte key:value to modify
	// or insert therefore colonOffset is 2 bytes, valueOffset is
	// 3 bytes into `incoming`.
	const colonOffset = 2
	const valueOffset = colonOffset + 1

	if incoming == "" {
		return updated
	}
	var out strings.Builder

	// The update is expected to be a single key-value of the form
	// `XX:value` for with two-character key.
	upkey := updated[:colonOffset]

	// In this case, there is an existing field under "ot" and we
	// need to combine.  We will pass the parts of "incoming"
	// through except the field we are updating, which we will
	// modify if it is found.
	foundUp := false

	for count := 0; len(incoming) != 0; count++ {
		key, rest, hasCol := strings.Cut(incoming, ":")
		if !hasCol {
			// return the updated value, ignore invalid inputs
			return updated
		}
		value, next, _ := strings.Cut(rest, ";")

		if key == upkey {
			value = updated[valueOffset:]
			foundUp = true
		}
		if count != 0 {
			out.WriteString(";")
		}
		out.WriteString(key)
		out.WriteString(":")
		out.WriteString(value)

		incoming = next
	}
	if !foundUp {
		out.WriteString(";")
		out.WriteString(updated)
	}
	return out.String()
}
