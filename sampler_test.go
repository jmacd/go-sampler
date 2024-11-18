// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sampler

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestSamplerDescription(t *testing.T) {
	type testCase struct {
		sampler     interface{ Description() string }
		description string
	}

	for _, test := range []testCase{
		{
			AlwaysSample(),
			"AlwaysOn",
		},
		{
			NeverSample(),
			"AlwaysOff",
		},
		{
			RuleBased(
				WithRule(SpanNamePredicate("/healthcheck"), ComposableNeverSample()),
				WithDefaultRule(ComposableAlwaysSample()),
			),
			"RuleBased{rule(Span.Name==/healthcheck)=AlwaysOff,rule(true)=AlwaysOn}",
		},
		{
			ConsistentParentBased(ComposableAlwaysSample()),
			"RuleBased{rule(root?)=AlwaysOn,rule(true)=ParentThreshold}",
		},
	} {
		require.Equal(t, test.description, test.sampler.Description())
	}
}

type testContext struct {
	context.Context
	SamplingParameters
}

type benchFuncs struct {
	sampled    func() bool
	remote     func() bool
	tracestate func() trace.TraceState
	name       func() string
	kind       func() trace.SpanKind
	attributes func() []attribute.KeyValue
	links      func() []trace.Link
}

func makeBenchContexts(
	n int,
	bfuncs benchFuncs,
) (
	r []testContext,
) {
	randSource := rand.New(rand.NewSource(101333))
	for range n {
		var cfg trace.SpanContextConfig
		randSource.Read(cfg.TraceID[:])
		randSource.Read(cfg.SpanID[:])
		if bfuncs.sampled() {
			cfg.TraceFlags = 3
		}
		if bfuncs.remote() {
			cfg.Remote = true
		}
		cfg.TraceState = bfuncs.tracestate()
		ctx := trace.ContextWithSpanContext(
			context.Background(),
			trace.NewSpanContext(cfg),
		)
		r = append(r, testContext{
			Context: ctx,
			SamplingParameters: SamplingParameters{
				ParentContext: ctx,
				TraceID:       cfg.TraceID,
				Name:          bfuncs.name(),
				Kind:          bfuncs.kind(),
				Attributes:    bfuncs.attributes(),
				Links:         bfuncs.links(),
			},
		})
	}
	return
}

func defaultBenchFuncs() benchFuncs {
	return benchFuncs{
		sampled: func() bool { return true },
		remote:  func() bool { return true },
		tracestate: func() trace.TraceState {
			ts, _ := trace.ParseTraceState("")
			return ts
		},
		name:       func() string { return "test" },
		kind:       func() trace.SpanKind { return trace.SpanKindInternal },
		attributes: func() []attribute.KeyValue { return nil },
		links:      func() []trace.Link { return nil },
	}
}

func makeSimpleContexts(n int) []testContext {
	return makeBenchContexts(n, defaultBenchFuncs())
}

func BenchmarkConsistentParentBased(b *testing.B) {
	ctxs := makeSimpleContexts(b.N)
	sampler := CompositeSampler(ConsistentParentBased(ComposableAlwaysSample()))
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i].SamplingParameters)
	}
}
