// Package xnoise provides deterministic, allocation-free hashing and value
// noise. Every visual in agentforest is seeded through here, so the same
// forest always grows the same way.
package xnoise

import "math"

// Hash mixes a seed with any number of keys using a splitmix64-style finalizer.
func Hash(seed uint64, ks ...uint64) uint64 {
	h := seed ^ 0x9e3779b97f4a7c15
	for _, k := range ks {
		h ^= k + 0x9e3779b97f4a7c15 + (h << 6) + (h >> 2)
		h = mix(h)
	}
	return mix(h)
}

func mix(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Unit returns a deterministic float in [0,1).
func Unit(seed uint64, ks ...uint64) float64 {
	return float64(Hash(seed, ks...)>>11) / float64(1<<53)
}

// Range returns a deterministic float in [lo,hi).
func Range(seed uint64, lo, hi float64, ks ...uint64) float64 {
	return lo + (hi-lo)*Unit(seed, ks...)
}

func smooth(t float64) float64 { return t * t * (3 - 2*t) }

func lattice1(seed uint64, xi int64) float64 {
	return float64(Hash(seed, uint64(xi))>>11) / float64(1<<53)
}

// Value1 is smooth 1-D value noise in [0,1].
func Value1(seed uint64, x float64) float64 {
	xf := math.Floor(x)
	xi := int64(xf)
	t := smooth(x - xf)
	a := lattice1(seed, xi)
	b := lattice1(seed, xi+1)
	return a + (b-a)*t
}

// FBM1 layers octaves of Value1, normalized to [0,1].
func FBM1(seed uint64, x float64, octaves int) float64 {
	sum, amp, freq, norm := 0.0, 1.0, 1.0, 0.0
	for i := 0; i < octaves; i++ {
		sum += amp * Value1(Hash(seed, uint64(i)), x*freq)
		norm += amp
		amp *= 0.5
		freq *= 2.1
	}
	return sum / norm
}

func lattice2(seed uint64, xi, yi int64) float64 {
	return float64(Hash(seed, uint64(xi), uint64(yi))>>11) / float64(1<<53)
}

// Value2 is smooth 2-D value noise in [0,1].
func Value2(seed uint64, x, y float64) float64 {
	xf, yf := math.Floor(x), math.Floor(y)
	xi, yi := int64(xf), int64(yf)
	tx, ty := smooth(x-xf), smooth(y-yf)
	a := lattice2(seed, xi, yi)
	b := lattice2(seed, xi+1, yi)
	c := lattice2(seed, xi, yi+1)
	d := lattice2(seed, xi+1, yi+1)
	ab := a + (b-a)*tx
	cd := c + (d-c)*tx
	return ab + (cd-ab)*ty
}

// FBM2 layers octaves of Value2, normalized to [0,1].
func FBM2(seed uint64, x, y float64, octaves int) float64 {
	sum, amp, freq, norm := 0.0, 1.0, 1.0, 0.0
	for i := 0; i < octaves; i++ {
		sum += amp * Value2(Hash(seed, uint64(i)), x*freq, y*freq)
		norm += amp
		amp *= 0.5
		freq *= 2.1
	}
	return sum / norm
}

// Smoothstep maps x from [lo,hi] to [0,1] with smooth ends.
func Smoothstep(lo, hi, x float64) float64 {
	if x <= lo {
		return 0
	}
	if x >= hi {
		return 1
	}
	return smooth((x - lo) / (hi - lo))
}

// Clamp bounds v to [lo,hi].
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Lerp interpolates a toward b by t.
func Lerp(a, b, t float64) float64 { return a + (b-a)*t }
