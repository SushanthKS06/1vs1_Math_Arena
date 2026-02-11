package rng

import (
	"math/rand"
)

type SeededRNG struct {
	rand *rand.Rand
}

func NewSeededRNG(seed int64) *SeededRNG {
	return &SeededRNG{
		rand: rand.New(rand.NewSource(seed)),
	}
}

func (r *SeededRNG) IntRange(min, max int) int {
	if min >= max {
		return min
	}
	return min + r.rand.Intn(max-min+1)
}

func (r *SeededRNG) Int() int {
	return r.rand.Int()
}

func (r *SeededRNG) Float64() float64 {
	return r.rand.Float64()
}

func (r *SeededRNG) Shuffle(n int, swap func(i, j int)) {
	r.rand.Shuffle(n, swap)
}
