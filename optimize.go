// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sampler

import (
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
)

func optimize1(s Sampler, res *resource.Resource, scope instrumentation.Scope) Sampler {
	if so, ok := s.(SamplerOptimizer); ok {
		return so.Optimize(res, scope)
	}
	return s
}

func optimize2(s ComposableSampler, res *resource.Resource, scope instrumentation.Scope) ComposableSampler {
	if cso, ok := s.(ComposableSamplerOptimizer); ok {
		return cso.Optimize(res, scope)
	}
	return s
}

func (as *annotatingSampler) Optimize(res *resource.Resource, scope instrumentation.Scope) ComposableSampler {
	return &annotatingSampler{
		attributes: as.attributes,
		sampler:    optimize2(as.sampler, res, scope),
	}

}

func (rb ruleBased) Optimize(res *resource.Resource, scope instrumentation.Scope) ComposableSampler {
	var opt []ruleAndPredicate
	for _, rule := range rb {
		// TODO: Here, what's next?
		// if rule.Predicate.Optimize(res, scope)
		// Note that there is even more potential missed, e.g.,
		// ability to optimize on span.Kind or really any conjunct.
		opt = append(opt, ruleAndPredicate{
			Predicate:         rule.Predicate,
			ComposableSampler: optimize2(rule.ComposableSampler, res, scope),
		})
	}
	return ruleBased(opt)
}

func (cs *compositeSampler) Optimize(res *resource.Resource, scope instrumentation.Scope) Sampler {
	return &compositeSampler{
		sampler: optimize2(cs.sampler, res, scope),
	}
}
