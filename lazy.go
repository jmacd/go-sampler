// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sampler

import "sync"

type evaluator[T any] func() T

type lazy[T any] struct {
	once      sync.Once
	evaluator evaluator[T]
	value     T
}

func newLazy[T any](evaluator evaluator[T]) lazy[T] {
	return lazy[T]{
		evaluator: evaluator,
	}
}

func (l *lazy[T]) Value() T {
	l.once.Do(func() {
		l.value = l.evaluator()
		l.evaluator = nil
	})

	return l.value
}
