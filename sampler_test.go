package sampler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSamplerDescription(t *testing.T) {
	type testCase struct {
		sampler     interface{ Description() string }
		description string
	}

	for _, test := range []testCase{
		{AlwaysSample(), "AlwaysOn"},
		{NeverSample(), "AlwaysOff"},
		{RuleBased(
			WithRule(SpanNamePredicate("/healthcheck"), ComposableNeverSample()),
			WithDefaultRule(ComposableAlwaysSample()),
		),
			"RuleBased{rule(Span.Name==/healthcheck)=AlwaysOff,rule(true)=AlwaysOn}",
		},
	} {
		require.Equal(t, test.description, test.sampler.Description())
	}
}
