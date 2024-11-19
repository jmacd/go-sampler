## Learnings

### Mutable tracestate problem

Do not let ComposableSampler modify TraceState, let that be a function
of the "full-fledged" Sampler API.

This makes for a couple of optimizations: 
1. Samplers can use the offset of the original trace threshold when updating. (Faster!)
2. Samplers can ask themselves "WouldSample()" as opposed to the global decision. (Optional?)

### Export-only problem

Need a new return value.
