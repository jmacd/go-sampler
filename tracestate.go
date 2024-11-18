// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sampler

import (
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
func combineTracestate(original trace.TraceState, updateThreshold int64) trace.TraceState {
	// There's nothing to do when original is empty and so is the new threshold.
	if original.Len() == 0 && updateThreshold < 0 {
		return original
	}

	unmodified := original.Get("ot")
	incoming := unmodified

	var out strings.Builder

	// This loop body copies the OpenTelemetry tracestate value
	// while erasing the incoming threshold.
	for len(incoming) != 0 {
		cpos := strings.IndexByte(incoming, ':')

		if cpos < 0 {
			// Awkward situation: we can't parse the
			// tracestate we're meant to modify.  Handle
			// an error and erase the entry entirely.
			otel.Handle(fmt.Errorf("cannot update invalid tracestate: %s", unmodified))
			return original.Delete("ot")
		}

		// Note: There are a few loose ends here related to
		// whitespace handling.  At the risk of unnecessary
		// code complexity, probably OTel's tracestate
		// specification should declare e.g., leading
		// whitespace invalid.
		thisKey := incoming[:cpos]
		toWrite := incoming

		if spos := strings.IndexByte(incoming[cpos+1:], ';'); spos < 0 {
			// This is the final pair.
			incoming = ""
			toWrite = incoming
		} else {
			// Note that the semicolon is not included in toWrite
			// but it is skipped in incoming, the difference is 1.
			split := cpos + spos + 1
			toWrite = incoming[:split]
			incoming = incoming[split+1:]
		}
		if thisKey == "th" {
			// Skip the incoming th value.
			continue
		}
		if out.Len() != 0 {
			out.WriteString(";")
		}
		out.WriteString(toWrite)
	}
	if updateThreshold >= 0 {
		if out.Len() != 0 {
			out.WriteString(";th:")
		} else {
			out.WriteString("th:")
		}
		if updateThreshold == 0 {
			out.WriteString("0")
		}
		out.WriteString(strings.TrimRight(strconv.FormatUint(uint64(updateThreshold), 16), "0"))
	}
	returnTracestate, err := original.Insert("ot", out.String())
	if err != nil {
		otel.Handle(fmt.Errorf("could not update tracestate with threshold: %w", err))
	}
	return returnTracestate
}
