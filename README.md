# OTel-Go Composable Sampler prototype

## Summary

This is a prototype in support of [OTEP 250](https://github.com/open-telemetry/oteps/pull/250).

This is at a "proof-of-concept" maturity level.  Testing coverage is
good but intentionally not complete.

As described in the OTEP, the use of consistent probability sampling
in complex scenarios can lead to inaccurate counting.  The machinery
needed to enforce consistent sampling so that it can be widely
deployed with reliable results is necessarily more complex.

This prototype demonstrates a solution tailored to the OTel-Go Trace
SDK.  Pieces of this implementation are copied verbatim from that SDK
to make the prototype useful in that context.  In particular, the
`Sampler`, `SamplingParameters`, `SamplingResult`, and
`SamplingDecision` types are preserved and the `AlwaysSample()` and
`ParentBased()` constructors are copied exactly, so the benchmarks can
be faithful.

This prototype was initiated for several reasons, one of which being
[questions in the previous prototype PR about
performance](https://github.com/open-telemetry/opentelemetry-go/pull/5645).

This implementation follows "Approach 2" described in the OTEP, which
is designed to optimize the cost and structure of consistent sampling
decision-making.  Another prototype for the same in Java can be found
[here](https://github.com/open-telemetry/opentelemetry-java-contrib/tree/main/consistent-sampling/src/main/java/io/opentelemetry/contrib/sampler/consistent56).

### Key Contributions

The goal of this prototype is to establish a backwards-compatible API
for Samplers, particularly for the existing `ParentBased` Sampler.
This was not directly addressed in the OTEP.  The reason this is
somewhat challenging is that the `ParentBased` specification accepts
`Sampler` instances to be called for unsampled contexts, and a
consistent (and unbiased) sampler cannot condition on the sampled
flag.

This prototype shows how `ParentBased` can be replaced by various
combinations of `Composite`, `Annotating`, `RuleBased`, and
`ParentThreshold` samplers.

#### ComposableSampler

This is a new `ComposableSampler` interface as described in the OTEP.
This sampler returns an intent as opposed to a decision.  Sampling
intentions can be combined, for example in an `AnyOf` sampler.

#### CompositeSampler

CompositeSampler constructs a `Sampler` from a `ComposableSampler`.
This is where the bulk of the logic involved in consistent sampling
happens.

One key optimization that can be found here: the `ComposableSampler`
does not have the ability to modify `TraceState` the way a `Sampler`
can.  This enables two optimizations:

1. Information about the parsed input `TraceState` can be used to
   construct a modified output `TraceState`.  There are situations
   where the `CompositeSampler` has to erase or modify the encoded
   threshold, and this can re-use the potision information from the
   parser if the `ComposableSampler` API prevents modifying TraceState.
2. The `AnnotatingSampler` interface can _potentially_ make use of the
   randomness value assuming it does not change as the result of a
   `ComposableSampler`.  The example where this matters: a sampler,
   part of an AnyOf construction, wishes to insert an attribute
   specifically when it would sample, not necessarily when there is a
   global decision to sample.  For a sampler to ask "would I sample?"
   the randomness value needs to be accessible in the parameters and
   cannot be mutated.
   
The upshot of this is that for a Root sampler to set the randomness
value, it will have to be done outside of the composite samplerinterface.

#### ComposableSamplingParameters

The `ComposableSamplingParameters` type combines the original
SamplingParameters with an optimization.  The original
SamplingParameters include the parent's `context.Context`, which
forces Samplers to lookup the SpanContext.  In a composite sampler,
this could happen multiple times, so it makes sense to include
`SpanContext` in the parameters directly.

This type includes non-exported copies of the effective incoming
threshold and the computed randomness value, with the following
rationale:

1. The incoming threshold value is the one that `ParentThreshold()`
   sampler will use.  It is the only sampler that uses this field, and
   its value is derived using inputs from the `ConsistentSampler`.
2. The incoming randomness value can be used for a sampler to ask
   whether it iself would sampler, see the `WouldSample(params
   ComposableSamplingParameters) bool` API.

#### TraceIdRatioBased

This aspect of the prototype is copied from the [first
prototype](https://github.com/open-telemetry/opentelemetry-go/pull/5645).
See the related, pending OpenTelemetry specification work in [PR
4166](https://github.com/open-telemetry/opentelemetry-specification/pull/4166)
and [PR
4162](https://github.com/open-telemetry/opentelemetry-specification/pull/4162).

#### RuleBased

This is as-described in the OTEP.  This is differs slightly from the
implementation found in the Java prototype in removing the `SpanKind`
argument from the rule because it can be treated as an ordinary aspect of the predicate.

See [potential optimizations discussed below](#potential-optimizations).

#### ParentThreshold

This is a special built-in Sampler that makes the same decision the
parent context did, which results in passing through consistent
sampling thresholds correctly.  As an example, the essential function
of `ParentBased` can be replaced as follows:

```
func ComposableParentBased(root ComposableSampler) ComposableSampler {
	return RuleBased(
		WithRule(IsRootPredicate(), root),
		WithDefaultRule(ParentThreshold()),
	)
}
```

#### AnnotatingSampler

This is a convenience implementation of `ComposableSampler` meant to
support adding span attributes while using `ComposableSampler` APIs.

TODO: This is not well tested.  This is where we could potentially use
the `WouldSample()` method above.

## Benchmarks

The benchmarks here are sufficient to identify the overhead introduced
by composable samplers and enforcing consistent sampling.  The
original OTel-Go AlwaysOn and ParentBased samplers are included as a
reference.

```
```

### Export-only problem

Need a new return value.


### Not explored

Optimizing for scope and resource

// NEW PROTOTYPE BELOW

// SamplerOptimizer is an optional interface to optimize Samplers.
type SamplerOptimizer interface {
	Optimize(*resource.Resource, instrumentation.Scope) Sampler
}

// ComposableSamplerOptimizer is an optional interface to optimize ComposableSamplers.
type ComposableSamplerOptimizer interface {
	Optimize(*resource.Resource, instrumentation.Scope) ComposableSampler
}

