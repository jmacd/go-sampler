package sampler

import (
	"context"
	"fmt"
	"math"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// V1 API

type SamplingParameters struct {
	ParentContext context.Context
	TraceID       trace.TraceID
	Name          string
	Kind          trace.SpanKind
	Attributes    []attribute.KeyValue
	Links         []trace.Link
}

type Sampler interface {
	ShouldSample(SamplingParameters) SamplingResult
	Description() string
}

type SamplingDecision uint8

const (
	Drop SamplingDecision = iota
	RecordOnly
	RecordAndSample
)

type SamplingResult struct {
	Decision   SamplingDecision
	Attributes []attribute.KeyValue
	Tracestate trace.TraceState
}

// Composable API

type ComposableSamplingParameters struct {
	SamplingParameters

	// Note: ParentSpanContext == trace.SpanContextFromContext(p.ParentContext)
	// this is an expensive call, so we compute it once in case multiple predicates
	// need it.

	ParentSpanContext trace.SpanContext

	SpanID trace.SpanID

	// not exported
	parentThreshold uint64
	traceRandomness uint64

	// Missing:
	// Resource
	// Scope

}

type ComposableSampler interface {
	GetSamplingIntent(ComposableSamplingParameters) SamplingIntent
	Description() string
}

type SamplingIntent struct {
	Record    bool   // whether to record
	Export    bool   // whether to export, implies record
	Threshold uint64 // i.e., sampling probability, implies record & export when...

	Attributes AttributesFunc

	// SampledTracestate AttributesFunc
	// UnsampledTracestate func(trace.TraceState) trace.TraceState
}

// TraceIDRatioBased

type traceIDRatio struct {
	// threshold is a rejection threshold.
	// Select when (T <= R)
	// Drop when (T > R)
	// Range is [0, 1<<56).
	threshold   uint64
	description string
}

const (
	// DefaultSamplingPrecision is the number of hexadecimal
	// digits of precision used to expressed the samplling probability.
	defaultSamplingPrecision = 4

	// MinSupportedProbability is the smallest probability that
	// can be encoded by this implementation, and it defines the
	// smallest interval between probabilities across the range.
	// The largest supported probability is (1-MinSupportedProbability).
	//
	// This value corresponds with the size of a float64
	// significand, because it simplifies this implementation to
	// restrict the probability to use 52 bits (vs 56 bits).
	minSupportedProbability float64 = 1 / float64(maxAdjustedCount)

	// maxSupportedProbability is the number closest to 1.0 (i.e.,
	// near 99.999999%) that is not equal to 1.0 in terms of the
	// float64 representation, having 52 bits of significand.
	// Other ways to express this number:
	//
	//   0x1.ffffffffffffe0p-01
	//   0x0.fffffffffffff0p+00
	//   math.Nextafter(1.0, 0.0)
	maxSupportedProbability float64 = 1 - 0x1p-52

	// maxAdjustedCount is the inverse of the smallest
	// representable sampling probability, it is the number of
	// distinct 56 bit values.
	maxAdjustedCount uint64 = 1 << 56

	// randomnessMask is a mask that selects the least-significant
	// 56 bits of a uint64.
	randomnessMask uint64 = maxAdjustedCount - 1
)

func TraceIDRatioBased(fraction float64) ComposableSampler {
	const (
		maxp  = 14                       // maximum precision is 56 bits
		defp  = defaultSamplingPrecision // default precision
		hbits = 4                        // bits per hex digit
	)

	if fraction > maxSupportedProbability {
		return AlwaysSample()
	}

	if fraction < minSupportedProbability {
		return NeverSample()
	}

	// Calculate the amount of precision needed to encode the
	// threshold with reasonable precision.
	//
	// 13 hex digits is the maximum reasonable precision, since
	// that equals 52 bits, the number of bits in the float64
	// significand.
	//
	// Frexp() normalizes both the fraction and one-minus the
	// fraction, because more digits of precision are needed in
	// both cases -- in these cases the threshold has all leading
	// '0' or 'f' characters.
	//
	// We know that `exp <= 0`.  If `exp <= -4`, there will be a
	// leading hex `0` or `f`.  For every multiple of -4, another
	// leading `0` or `f` appears, so this raises precision
	// accordingly.
	_, expF := math.Frexp(fraction)
	precision := min(maxp, defp+expF/-hbits)

	// Compute the threshold
	scaled := uint64(math.Round(fraction * float64(maxAdjustedCount)))
	threshold := maxAdjustedCount - scaled

	// Round to the specified precision, if less than the maximum.
	if shift := hbits * (maxp - precision); shift != 0 {
		half := uint64(1) << (shift - 1)
		threshold += half
		threshold >>= shift
		threshold <<= shift
	}

	return &traceIDRatio{
		threshold:   threshold,
		description: fmt.Sprintf("TraceIDRatioBased{%g}", fraction),
	}
}

func (ts *traceIDRatio) Description() string {
	return ts.description
}

func (ts *traceIDRatio) GetSamplingIntent(p ComposableSamplingParameters) SamplingIntent {
	return SamplingIntent{
		Threshold: ts.threshold,
	}
}

// AlwaysOn

func AlwaysSample() ComposableSampler {
	return alwaysOn{}
}

type alwaysOn struct{}

func (alwaysOn) GetSamplingIntent(ComposableSamplingParameters) SamplingIntent {
	return SamplingIntent{
		Threshold: 0,
	}
}

func (alwaysOn) Description() string {
	return "AlwaysOn"
}

// AlwaysOff

func NeverSample() ComposableSampler {
	return alwaysOff{}
}

type alwaysOff struct{}

func (alwaysOff) GetSamplingIntent(ComposableSamplingParameters) SamplingIntent {
	return SamplingIntent{
		Threshold: maxAdjustedCount,
	}
}

func (alwaysOff) Description() string {
	return "AlwaysOff"
}

// RuleBased

func RuleBased(options ...RuleBasedOption) ComposableSampler {
	rbc := &ruleBasedConfig{}
	for _, opt := range options {
		opt(rbc)
	}
	if rbc.defRule != nil {
		rbc.rules = append(rbc.rules, ruleAndPredicate{
			Predicate:         TruePredicate(),
			ComposableSampler: rbc.defRule,
		})
	}
	return ruleBased(rbc.rules)
}

type ruleAndPredicate struct {
	Predicate
	ComposableSampler
}

type ruleBasedConfig struct {
	rules   []ruleAndPredicate
	defRule ComposableSampler
}

type ruleBased []ruleAndPredicate

func (rb ruleBased) Description() string {
	return fmt.Sprintf("RuleBased{%s}",
		strings.Join(func(rules []ruleAndPredicate) (desc []string) {
			for _, rule := range rules {
				desc = append(desc,
					fmt.Sprintf("rule(%s)=%s",
						rule.Predicate.Description(),
						rule.ComposableSampler.Description(),
					),
				)
			}
			return
		}(rb), ","))
}

func (rb ruleBased) GetSamplingIntent(params ComposableSamplingParameters) SamplingIntent {
	for _, rule := range rb {
		if rule.Decide(params) {
			return rule.ComposableSampler.GetSamplingIntent(params)
		}
	}

	// When no rules match.  This will not happen when there is a
	// default rule set.
	return SamplingIntent{
		Threshold: maxAdjustedCount,
	}
}

// ConsistentParentBased combines a root sampler and a ParentThreshold.
func ConsistentParentBased(root ComposableSampler) ComposableSampler {
	return RuleBased(
		WithRule(IsRootPredicate(), root),
		WithDefaultRule(ParentThreshold()),
	)
}

// ParentThreshold may be composed to form consistent parent-based sampling.
func ParentThreshold() ComposableSampler {
	return parentThreshold{}
}

type parentThreshold struct{}

func (parentThreshold) GetSamplingIntent(params ComposableSamplingParameters) SamplingIntent {
	// @@@ problem? if the parent is a legacy, no threshold comes in.
	// how does this decision get made?
	return SamplingIntent{
		Threshold: params.parentThreshold,
	}
}

func (parentThreshold) Description() string {
	return "ParentThreshold"
}

// Annotating ("Marker")

type AnnotatingOption func(*annotatingConfig)

type AttributesFunc func() []attribute.KeyValue

type annotatingConfig struct {
	attributes AttributesFunc
}

type annotatingSampler struct {
	sampler    ComposableSampler
	attributes AttributesFunc
}

func AnnotatingSampler(sampler ComposableSampler, options ...AnnotatingOption) ComposableSampler {
	var config annotatingConfig
	for _, opt := range options {
		opt(&config)
	}
	return &annotatingSampler{
		sampler:    sampler,
		attributes: config.attributes,
	}
}

func combineAttributesFunc(one, two AttributesFunc) AttributesFunc {
	return func() []attribute.KeyValue {
		if one == nil && two == nil {
			return nil
		}
		if one == nil {
			return two()
		}
		if two == nil {
			return one()
		}
		return append(one(), two()...)
	}
}

func WithSampledAttributes(af AttributesFunc) AnnotatingOption {
	return func(cfg *annotatingConfig) {
		cfg.attributes = combineAttributesFunc(cfg.attributes, af)
	}
}

func (as annotatingSampler) GetSamplingIntent(params ComposableSamplingParameters) SamplingIntent {
	intent := as.sampler.GetSamplingIntent(params)
	intent.Attributes = combineAttributesFunc(intent.Attributes, as.attributes)
	return intent
}

func (as annotatingSampler) Description() string {
	return fmt.Sprintf("Annotating(%s)", as.sampler.Description())
}
