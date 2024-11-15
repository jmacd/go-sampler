package sampler

import "sync"

type supplier[T any] func() T

type lazy[T any] struct {
	once     sync.Once
	supplier supplier[T]
	value    T
}

func newLazy[T any](supplier supplier[T]) lazy[T] {
	return lazy[T]{
		supplier: supplier,
	}
}

func (l *lazy[T]) Value() T {
	l.once.Do(func() {
		l.value = l.supplier()
		l.supplier = nil
	})

	return l.value
}
