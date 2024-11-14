package sampler

import (
	"context"
	"fmt"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// V1 API

type SamplingParametersV1 struct {
	ParentContext context.Context
	TraceID       trace.TraceID
	Name          string
	Kind          trace.SpanKind
	Attributes    []attribute.KeyValue
	Links         []trace.Link
}

type SamplerV1 interface {
	ShouldSampleV1(SamplingParametersV1) SamplingResultV1
	Description() string
}

type SamplingDecisionV1 uint8

const (
	Drop SamplingDecisionV1 = iota
	RecordOnly
	RecordAndSample
)

type SamplingResultV1 struct {
	Decision   SamplingDecisionV1
	Attributes []attribute.KeyValue
	Tracestate trace.TraceState
}

// V2 API

type SamplingParametersV2 struct {
	SamplingParametersV1

	// Threshold  uint64
	// Randomness uint64

	SpanID trace.SpanID

	// NO:
	// Resource
	// Scope
}

type SamplerV2 interface {
	ShouldSampleV2(SamplingParametersV2) SamplingResultV2
	Description() string
}

type SamplingResultV2 struct {
	Record     bool   // whether to record
	Export     bool   // whether to export, implies record
	Threshold  uint64 // i.e., probability, implies record & export when...
	Attributes func([]attribute.KeyValue) []attribute.KeyValue
	Tracestate func(trace.TraceState) trace.TraceState
}

// AlwaysOn V2

type alwaysOn struct{}

func AlwaysSample() SamplerV2 {
	return alwaysOn{}
}

func (alwaysOn) ShouldSampleV2(SamplingParametersV2) SamplingResultV2 {
	return SamplingResultV2{
		Record:     true,
		Export:     true,
		Threshold:  0, // "0" means always sampled
		Attributes: nil,
		Tracestate: nil,
	}
}

func (alwaysOn) Description() string {
	return "AlwaysOn"
}

// AlwaysOff V2

type alwaysOff struct{}

func NeverSample() SamplerV2 {
	return nil
}

func (alwaysOff) ShouldSampleV2(SamplingParametersV2) SamplingResultV2 {
	return SamplingResultV2{
		Record:    false,
		Export:    false,
		Threshold: maxAdjustedCount, // this threshold never samples
	}
}

func (alwaysOff) Description() string {
	return "AlwaysOff"
}

// TraceIDRatioBased V2

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

func TraceIDRatioBased(fraction float64) SamplerV2 {
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

func (ts *traceIDRatio) ShouldSampleV2(p SamplingParametersV2) SamplingResultV2 {
	return SamplingResultV2{
		Export:    false,
		Record:    false,
		Threshold: ts.threshold,
	}
}

// ParentBased V2

type parentBased struct {
	root, remoteYes, remoteNo, localYes, localNo SamplerV2
}

func ParentBased(root SamplerV2, options ...ParentBasedSamplerOption) SamplerV2 {
	pb := parentBased{
		root:      root,
		remoteYes: AlwaysSample(),
		remoteNo:  NeverSample(),
		localYes:  AlwaysSample(),
		localNo:   NeverSample(),
	}
	for _, opt := range options {
		pb = opt.apply(pb)
	}

	return pb
}

func (pb parentBased) ShouldSampleV2(p SamplingParametersV2) SamplingResultV2 {
	psc := trace.SpanContextFromContext(p.ParentContext)
	if psc.IsValid() {
		if psc.IsRemote() {
			if psc.IsSampled() {
				return pb.remoteYes.ShouldSampleV2(p)
			}
			return pb.remoteNo.ShouldSampleV2(p)
		}

		if psc.IsSampled() {
			return pb.localYes.ShouldSampleV2(p)
		}
		return pb.localNo.ShouldSampleV2(p)
	}
	return pb.root.ShouldSampleV2(p)
}

func (pb parentBased) Description() string {
	return fmt.Sprintf("ParentBased{root:%s,remoteParentSampled:%s,"+
		"remoteParentNotSampled:%s,localParentSampled:%s,localParentNotSampled:%s}",
		pb.root.Description(),
		pb.remoteYes.Description(),
		pb.remoteNo.Description(),
		pb.localYes.Description(),
		pb.localNo.Description(),
	)
}

// ParentBasedSamplerOption configures the sampler for a particular sampling case.
type ParentBasedSamplerOption interface {
	apply(parentBased) parentBased
}

// WithRemoteParentSampled sets the sampler for the case of sampled remote parent.
func WithRemoteParentSampled(s SamplerV2) ParentBasedSamplerOption {
	return remoteParentSampledOption{s}
}

type remoteParentSampledOption struct{ SamplerV2 }

func (o remoteParentSampledOption) apply(sampler parentBased) parentBased {
	sampler.remoteYes = o.SamplerV2
	return sampler
}

// WithRemoteParentNotSampled sets the sampler for the case of remote parent
// which is not sampled.
func WithRemoteParentNotSampled(s SamplerV2) ParentBasedSamplerOption {
	return remoteParentNotSampledOption{s}
}

type remoteParentNotSampledOption struct{ SamplerV2 }

func (o remoteParentNotSampledOption) apply(sampler parentBased) parentBased {
	sampler.remoteNo = o.SamplerV2
	return sampler
}

// WithLocalParentSampled sets the sampler for the case of sampled local parent.
func WithLocalParentSampled(s SamplerV2) ParentBasedSamplerOption {
	return localParentSampledOption{s}
}

type localParentSampledOption struct{ SamplerV2 }

func (o localParentSampledOption) apply(sampler parentBased) parentBased {
	sampler.localYes = o.SamplerV2
	return sampler
}

// WithLocalParentNotSampled sets the sampler for the case of local parent
// which is not sampled.
func WithLocalParentNotSampled(s SamplerV2) ParentBasedSamplerOption {
	return localParentNotSampledOption{s}
}

type localParentNotSampledOption struct{ SamplerV2 }

func (o localParentNotSampledOption) apply(sampler parentBased) parentBased {
	sampler.localNo = o.SamplerV2
	return sampler
}
