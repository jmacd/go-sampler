// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package sampler

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var testAttrs = []attribute.KeyValue{attribute.String("K", "V")}

var testTs = func() trace.TraceState {
	ts, err := trace.ParseTraceState("tsk1=tsv,tsk2=tsv")
	if err != nil {
		panic(err)
	}
	return ts
}()

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

func TestAlwaysSample(t *testing.T) {
	for _, sampler := range []Sampler{
		AlwaysSample(),
		CompositeSampler(ComposableAlwaysSample()),
	} {
		t.Run(fmt.Sprintf("%T", sampler), func(t *testing.T) {
			test := defaultTestFuncs()
			test.tracestate = func() trace.TraceState {
				return testTs
			}
			test.attributes = func() []attribute.KeyValue {
				return testAttrs
			}
			params := makeTestContext(test).SamplingParameters
			sampler := AlwaysSample()

			result := sampler.ShouldSample(params)
			require.Equal(t, RecordAndSample, result.Decision)
			require.Empty(t, result.Attributes)
			require.Equal(t, testTs, result.Tracestate)
		})
	}
}

func TestParentBased(t *testing.T) {
	// for _, sampler := range []Sampler{
	// 	AlwaysSample(),
	// 	CompositeSampler(ComposableAlwaysSample()),
	// } {
	// 	t.Run(fmt.Sprintf("%T", sampler), func(t *testing.T) {
	// 		test := defaultTestFuncs()
	// 		test.tracestate = func() trace.TraceState {
	// 			return testTs
	// 		}
	// 		test.attributes = func() []attribute.KeyValue {
	// 			return testAttrs
	// 		}
	// 		params := makeTestContext(test).SamplingParameters
	// 		sampler := AlwaysSample()

	// 		result := sampler.ShouldSample(params)
	// 		require.Equal(t, RecordAndSample, result.Decision)
	// 		require.Empty(t, result.Attributes)
	// 		require.Equal(t, testTs, result.Tracestate)
	// 	})
	// }
}

type testContext struct {
	context.Context
	SamplingParameters
}

type testFuncs struct {
	sampled    func() bool
	remote     func() bool
	tracestate func() trace.TraceState
	name       func() string
	kind       func() trace.SpanKind
	attributes func() []attribute.KeyValue
	links      func() []trace.Link
}

const maxContexts = 10000

func makeBenchContexts(
	n int,
	bfuncs testFuncs,
) (
	r []testContext,
) {
	randSource := rand.New(rand.NewSource(101333))
	for range max(n, maxContexts) {
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

func defaultTestFuncs() testFuncs {
	return testFuncs{
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
	return makeBenchContexts(n, defaultTestFuncs())
}

func makeTestContext(bf testFuncs) testContext {
	return makeBenchContexts(1, bf)[0]
}

func BenchmarkAlwaysOn(b *testing.B) {
	ctxs := makeSimpleContexts(b.N)
	sampler := AlwaysSample()
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkConsistentAlwaysOn(b *testing.B) {
	ctxs := makeSimpleContexts(b.N)
	sampler := CompositeSampler(ComposableAlwaysSample())
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkConsistentParentBased(b *testing.B) {
	ctxs := makeSimpleContexts(b.N)
	sampler := CompositeSampler(ConsistentParentBased(ComposableAlwaysSample()))
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkConsistentParentBasedWithNonEmptyTraceState(b *testing.B) {
	bfs := defaultTestFuncs()
	bfs.tracestate = func() trace.TraceState {
		ts, err := trace.ParseTraceState("co=whateverr,ed=nowaysir")
		require.NoError(b, err)
		return ts
	}
	ctxs := makeBenchContexts(b.N, bfs)
	sampler := CompositeSampler(ConsistentParentBased(ComposableAlwaysSample()))
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkConsistentParentBasedWithNonEmptyOTelTraceState(b *testing.B) {
	bfs := defaultTestFuncs()
	bfs.tracestate = func() trace.TraceState {
		ts, err := trace.ParseTraceState("co=whateverr,ed=nowaysir,ot=xx:abc;yy:def")
		require.NoError(b, err)
		return ts
	}
	ctxs := makeBenchContexts(b.N, bfs)
	sampler := CompositeSampler(ConsistentParentBased(ComposableAlwaysSample()))
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkConsistentParentBasedWithNonEmptyOTelTraceStateIncludingRandomness(b *testing.B) {
	bfs := defaultTestFuncs()
	bfs.tracestate = func() trace.TraceState {
		ts, err := trace.ParseTraceState("co=whateverr,ed=nowaysir,ot=xx:abc;yy:def;rv:abcdefabcdefab")
		require.NoError(b, err)
		return ts
	}
	ctxs := makeBenchContexts(b.N, bfs)
	sampler := CompositeSampler(ConsistentParentBased(ComposableAlwaysSample()))
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}

func BenchmarkParentBasedWithOTelTraceStateIncludingRandomness(b *testing.B) {
	bfs := defaultTestFuncs()
	bfs.tracestate = func() trace.TraceState {
		ts, err := trace.ParseTraceState("co=whateverr,ed=nowaysir,ot=xx:abc;yy:def;rv:abcdefabcdefab")
		require.NoError(b, err)
		return ts
	}
	ctxs := makeBenchContexts(b.N, bfs)
	sampler := ParentBased(AlwaysSample())
	b.ResetTimer()
	for i := range b.N {
		_ = sampler.ShouldSample(ctxs[i%maxContexts].SamplingParameters)
	}
}
