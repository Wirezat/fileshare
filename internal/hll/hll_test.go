package hll

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// stream returns a generator of distinct pseudo-random 64-bit hashes; the same seed yields the same stream.
func stream(seed uint64) func() uint64 {
	return func() uint64 {
		seed += 0x9e3779b97f4a7c15
		z := seed
		z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
		z = (z ^ z>>27) * 0x94d049bb133111eb
		return z ^ z>>31
	}
}

func filled(t *testing.T, p, n int, seed uint64) *Sketch {
	t.Helper()
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	next := stream(seed)
	for range n {
		s.Add(next())
	}
	return s
}

func TestNewRejectsPrecisionOutOfRange(t *testing.T) {
	for _, p := range []int{MinPrecision - 1, MaxPrecision + 1, 0, -1} {
		if _, err := New(p); !errors.Is(err, ErrPrecision) {
			t.Errorf("New(%d) err = %v, want ErrPrecision", p, err)
		}
	}
	for p := MinPrecision; p <= MaxPrecision; p++ {
		s, err := New(p)
		if err != nil || s.Precision() != p || len(s.regs) != 1<<p {
			t.Errorf("New(%d) = %v, %v", p, s, err)
		}
	}
}

func TestEmptySketchEstimatesZero(t *testing.T) {
	s := filled(t, 14, 0, 1)
	if got := s.Estimate(); got != 0 {
		t.Errorf("Estimate() = %v, want 0", got)
	}
	if !s.Empty() {
		t.Error("Empty() = false for a new sketch")
	}
	s.Add(42)
	if s.Empty() {
		t.Error("Empty() = true after Add")
	}
}

func TestAddSetsRegisterFromLeadingZeros(t *testing.T) {
	s, _ := New(4)
	s.Add(0x3<<60 | 1<<55)
	if got := s.regs[3]; got != 5 {
		t.Errorf("register 3 = %d, want 5", got)
	}
	s.Add(0x3<<60 | 1<<58)
	if got := s.regs[3]; got != 5 {
		t.Errorf("a smaller rank lowered register 3 to %d", got)
	}
	s.Add(0x7 << 60)
	if got := s.regs[7]; got != 61 {
		t.Errorf("all-zero tail gave register 7 = %d, want 61", got)
	}
}

func TestDuplicatesDoNotCount(t *testing.T) {
	s := filled(t, 14, 5000, 7)
	before := s.Estimate()
	next := stream(7)
	for range 5000 {
		s.Add(next())
	}
	if after := s.Estimate(); after != before {
		t.Errorf("re-adding the same items moved the estimate from %v to %v", before, after)
	}
}

// TestAccuracyAgainstKnownSets checks mean bias and spread of the estimate over many sets of known size.
func TestAccuracyAgainstKnownSets(t *testing.T) {
	cases := []struct {
		p    int
		n    int
		runs int
	}{
		{8, 10, 200}, {8, 100, 200}, {8, 300, 200}, {8, 700, 200}, {8, 1200, 200}, {8, 5000, 200}, {8, 100000, 50},
		{14, 10, 20}, {14, 1000, 20}, {14, 11000, 20}, {14, 20000, 20}, {14, 40000, 20}, {14, 70000, 20},
		{14, 200000, 10}, {14, 1000000, 4},
	}
	for _, c := range cases {
		stdErr := 1.04 / math.Sqrt(float64(int(1)<<c.p))
		var sum, sumSq float64
		for run := range c.runs {
			got := filled(t, c.p, c.n, uint64(c.p*1_000_000+c.n*1000+run)).Estimate()
			rel := got/float64(c.n) - 1
			sum += rel
			sumSq += rel * rel
		}
		mean := sum / float64(c.runs)
		rms := math.Sqrt(sumSq / float64(c.runs))
		if limit := 4 * stdErr / math.Sqrt(float64(c.runs)); math.Abs(mean) > limit {
			t.Errorf("p=%d n=%d: mean relative error %.4f exceeds %.4f", c.p, c.n, mean, limit)
		}
		if limit := 1.5 * stdErr; rms > limit {
			t.Errorf("p=%d n=%d: rms relative error %.4f exceeds %.4f", c.p, c.n, rms, limit)
		}
	}
}

func TestSmallSetsAreNearlyExact(t *testing.T) {
	for _, n := range []int{1, 2, 5, 20, 50} {
		got := filled(t, 14, n, uint64(n)).Estimate()
		if math.Abs(got-float64(n)) > 0.5 {
			t.Errorf("n=%d: Estimate() = %.2f", n, got)
		}
	}
}

func TestMergeEqualsSketchOfUnion(t *testing.T) {
	a := filled(t, 14, 30000, 1)
	b := filled(t, 14, 30000, 2)
	union, _ := New(14)
	for _, seed := range []uint64{1, 2} {
		next := stream(seed)
		for range 30000 {
			union.Add(next())
		}
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.regs, union.regs) {
		t.Fatal("merged registers differ from the sketch of the union")
	}
	if got := a.Estimate(); math.Abs(got/60000-1) > 3*1.04/128 {
		t.Errorf("union estimate %.0f, want about 60000", got)
	}
}

func TestMergeOverlapCountsSharedItemsOnce(t *testing.T) {
	a := filled(t, 14, 20000, 5)
	b := filled(t, 14, 20000, 5)
	before := a.Estimate()
	a.Merge(b)
	if got := a.Estimate(); got != before {
		t.Errorf("merging an identical sketch changed the estimate from %v to %v", before, got)
	}
}

func TestMergeRejectsOtherPrecision(t *testing.T) {
	a, _ := New(14)
	b, _ := New(8)
	if err := a.Merge(b); !errors.Is(err, ErrMismatch) {
		t.Errorf("Merge err = %v, want ErrMismatch", err)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	a := filled(t, 8, 100, 3)
	c := a.Clone()
	c.Add(1 << 63)
	c.Add(1<<62 | 1)
	if bytes.Equal(a.regs, c.regs) {
		t.Error("Add on the clone changed the original")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	for _, c := range []struct {
		p, n     int
		wantKind byte
	}{
		{14, 0, encSparse}, {14, 3, encSparse}, {14, 1000, encSparse}, {14, 200000, encDense},
		{8, 0, encSparse}, {8, 30, encSparse}, {8, 5000, encDense},
	} {
		s := filled(t, c.p, c.n, uint64(c.n))
		data, err := s.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if data[0] != c.wantKind {
			t.Errorf("p=%d n=%d: encoding %d, want %d", c.p, c.n, data[0], c.wantKind)
		}
		if len(data) > 2+len(s.regs) {
			t.Errorf("p=%d n=%d: encoding is %d bytes, longer than dense", c.p, c.n, len(data))
		}
		var back Sketch
		if err := back.UnmarshalBinary(data); err != nil {
			t.Fatalf("p=%d n=%d: %v", c.p, c.n, err)
		}
		if back.p != s.p || !bytes.Equal(back.regs, s.regs) {
			t.Errorf("p=%d n=%d: round trip changed the sketch", c.p, c.n)
		}
	}
}

func TestUnmarshalRejectsMalformed(t *testing.T) {
	dense8 := append([]byte{encDense, 8}, make([]byte, 256)...)
	tooHigh := append([]byte(nil), dense8...)
	tooHigh[10] = 58
	for name, data := range map[string][]byte{
		"empty":             nil,
		"short":             {encDense},
		"unknown kind":      {9, 8},
		"bad precision":     {encSparse, 3},
		"dense too short":   dense8[:200],
		"dense too long":    append(append([]byte(nil), dense8...), 0),
		"register too big":  tooHigh,
		"sparse truncated":  {encSparse, 8, 5},
		"sparse zero gap":   {encSparse, 8, 5, 1, 0, 1},
		"sparse past end":   {encSparse, 8, 0x80, 0x02, 1},
		"sparse zero":       {encSparse, 8, 5, 0},
		"sparse bad varint": {encSparse, 8, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	} {
		s := filled(t, 8, 10, 1)
		before := append([]byte(nil), s.regs...)
		if err := s.UnmarshalBinary(data); err == nil {
			t.Errorf("%s: UnmarshalBinary accepted %v", name, data)
		}
		if !bytes.Equal(s.regs, before) {
			t.Errorf("%s: a rejected encoding changed the sketch", name)
		}
	}
}

func TestEstimateBiasAveragesNearestNeighbours(t *testing.T) {
	saved := biasTables[0]
	defer func() { biasTables[0] = saved }()
	biasTables[0] = biasTable{
		raw:  []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
		bias: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}
	for _, c := range []struct{ raw, want float64 }{
		{0, 3.5}, {12, 3.5}, {44, 4.5}, {54, 5.5}, {56, 5.5}, {1000, 7.5},
	} {
		if got := estimateBias(c.raw, MinPrecision); got != c.want {
			t.Errorf("estimateBias(%v) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestBiasTablesCoverEveryPrecision(t *testing.T) {
	if len(biasTables) != MaxPrecision-MinPrecision+1 {
		t.Fatalf("%d tables for %d precisions", len(biasTables), MaxPrecision-MinPrecision+1)
	}
	for i, tb := range biasTables {
		p := i + MinPrecision
		m := float64(int(1) << p)
		if len(tb.raw) != len(tb.bias) || len(tb.raw) < biasNeighbours {
			t.Errorf("p=%d: %d raw and %d bias points", p, len(tb.raw), len(tb.bias))
			continue
		}
		for j := 1; j < len(tb.raw); j++ {
			if tb.raw[j] <= tb.raw[j-1] {
				t.Errorf("p=%d: raw estimates not increasing at %d", p, j)
				break
			}
		}
		if last := tb.raw[len(tb.raw)-1]; last < 5*m {
			t.Errorf("p=%d: table ends at %.0f, below 5m = %.0f", p, last, 5*m)
		}
		if first := tb.raw[0]; math.Abs(first-alpha(int(m))*m) > 0.01*m {
			t.Errorf("p=%d: raw estimate of the empty sketch %.1f, want alpha·m = %.1f", p, first, alpha(int(m))*m)
		}
	}
}
