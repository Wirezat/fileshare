// Package hll estimates distinct counts with dense HyperLogLog++ (Heule, Nunkesser, Hall 2013):
// 2^p registers, empirical bias correction and linear counting; callers pass finished 64-bit hashes.
package hll

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
)

const (
	MinPrecision = 4
	MaxPrecision = 14
)

var (
	ErrPrecision = errors.New("hll: precision out of range")
	ErrMismatch  = errors.New("hll: sketches have different precisions")
	ErrEncoding  = errors.New("hll: malformed encoding")
)

// Sketch is a HyperLogLog++ sketch with 2^p one-byte registers. It is not safe for concurrent use.
type Sketch struct {
	p    uint8
	regs []uint8
}

// New returns an empty sketch with 2^p registers; p must lie in [MinPrecision, MaxPrecision].
func New(p int) (*Sketch, error) {
	if p < MinPrecision || p > MaxPrecision {
		return nil, fmt.Errorf("%w: %d", ErrPrecision, p)
	}
	return &Sketch{p: uint8(p), regs: make([]uint8, 1<<p)}, nil
}

// Precision returns p, the base-2 logarithm of the register count.
func (s *Sketch) Precision() int { return int(s.p) }

// Add records one item given as its 64-bit hash.
func (s *Sketch) Add(hash uint64) {
	idx := hash >> (64 - s.p)
	w := hash<<s.p | 1<<(s.p-1)
	rho := uint8(bits.LeadingZeros64(w) + 1)
	if rho > s.regs[idx] {
		s.regs[idx] = rho
	}
}

// Merge folds other into s, so s then estimates the size of the union; both need the same precision.
func (s *Sketch) Merge(other *Sketch) error {
	if other.p != s.p {
		return ErrMismatch
	}
	for i, r := range other.regs {
		if r > s.regs[i] {
			s.regs[i] = r
		}
	}
	return nil
}

// Clone returns an independent copy of s.
func (s *Sketch) Clone() *Sketch {
	return &Sketch{p: s.p, regs: append([]uint8(nil), s.regs...)}
}

// Empty reports whether no item was ever added.
func (s *Sketch) Empty() bool {
	for _, r := range s.regs {
		if r != 0 {
			return false
		}
	}
	return true
}

// Estimate returns the estimated number of distinct items added.
func (s *Sketch) Estimate() float64 {
	m := float64(len(s.regs))
	raw, zeros := s.rawEstimate()
	e := raw
	if raw <= 5*m {
		e -= estimateBias(raw, int(s.p))
	}
	h := e
	if zeros > 0 {
		h = linearCounting(m, float64(zeros))
	}
	if h <= threshold[s.p] {
		return h
	}
	return e
}

// rawEstimate returns the plain HyperLogLog estimate and the number of empty registers.
func (s *Sketch) rawEstimate() (float64, int) {
	sum, zeros := 0.0, 0
	for _, r := range s.regs {
		sum += math.Ldexp(1, -int(r))
		if r == 0 {
			zeros++
		}
	}
	m := float64(len(s.regs))
	return alpha(len(s.regs)) * m * m / sum, zeros
}

func alpha(m int) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	}
	return 0.7213 / (1 + 1.079/float64(m))
}

func linearCounting(m, zeros float64) float64 {
	return m * math.Log(m/zeros)
}

// threshold is the cardinality below which linear counting beats the bias-corrected estimate, per precision.
//go:generate go run gen_bias.go

var threshold = [...]float64{4: 10, 20, 40, 80, 220, 400, 900, 1800, 3100, 6500, 11500}

// estimateBias returns the mean bias of the raw estimates nearest to raw, read from the table for p.
func estimateBias(raw float64, p int) float64 {
	t := biasTables[p-MinPrecision]
	i := sort.SearchFloat64s(t.raw, raw)
	lo, hi := i, i
	for hi-lo < biasNeighbours && (lo > 0 || hi < len(t.raw)) {
		if lo == 0 || (hi < len(t.raw) && t.raw[hi]-raw < raw-t.raw[lo-1]) {
			hi++
		} else {
			lo--
		}
	}
	sum := 0.0
	for _, b := range t.bias[lo:hi] {
		sum += b
	}
	return sum / float64(hi-lo)
}

const biasNeighbours = 6

type biasTable struct {
	raw, bias []float64
}

const (
	encDense  = 1
	encSparse = 2
)

// MarshalBinary encodes the sketch as its precision and registers, listing only non-empty registers when that is shorter.
func (s *Sketch) MarshalBinary() ([]byte, error) {
	sparse := []byte{encSparse, s.p}
	prev := 0
	for i, r := range s.regs {
		if r == 0 {
			continue
		}
		sparse = binary.AppendUvarint(sparse, uint64(i-prev))
		sparse = append(sparse, r)
		prev = i
		if len(sparse) >= 2+len(s.regs) {
			return append([]byte{encDense, s.p}, s.regs...), nil
		}
	}
	return sparse, nil
}

// UnmarshalBinary replaces s with the sketch encoded in data by MarshalBinary.
func (s *Sketch) UnmarshalBinary(data []byte) error {
	if len(data) < 2 {
		return ErrEncoding
	}
	p := int(data[1])
	fresh, err := New(p)
	if err != nil {
		return err
	}
	maxRho := uint8(64 - p + 1)
	switch body := data[2:]; data[0] {
	case encDense:
		if len(body) != len(fresh.regs) {
			return ErrEncoding
		}
		copy(fresh.regs, body)
	case encSparse:
		idx := 0
		for first := true; len(body) > 0; first = false {
			delta, n := binary.Uvarint(body)
			if n <= 0 || len(body) < n+1 || body[n] == 0 || (!first && delta == 0) || delta >= uint64(len(fresh.regs)-idx) {
				return ErrEncoding
			}
			idx += int(delta)
			fresh.regs[idx] = body[n]
			body = body[n+1:]
		}
	default:
		return ErrEncoding
	}
	for _, r := range fresh.regs {
		if r > maxRho {
			return ErrEncoding
		}
	}
	*s = *fresh
	return nil
}
