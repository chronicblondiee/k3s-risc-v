package ime

import (
	"errors"
	"math/rand"
	"testing"
)

func TestHasIMECPUInfo(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "uarch", in: "uarch\t: spacemit,x60\n", want: true},
		{name: "vendor", in: "mvendorid\t: 0x710\n", want: true},
		{name: "other", in: "uarch\t: other\nmvendorid\t: 0x0\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasIMECPUInfo(tt.in); got != tt.want {
				t.Fatalf("hasIMECPUInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReferenceKnownValues(t *testing.T) {
	var a, b [32]byte
	for i := range a {
		a[i] = byte(i - 16)
		b[i] = byte(31 - i)
	}
	for _, variant := range Variants() {
		var got [16]int32
		if err := ReferenceMul4x8(&got, &a, &b, variant); err != nil {
			t.Fatal(err)
		}
		for row := 0; row < 4; row++ {
			for col := 0; col < 4; col++ {
				var want int32
				for k := 0; k < 8; k++ {
					want += widenA(a[row*8+k], variant) * widenB(b[col*8+k], variant)
				}
				if got[row*4+col] != want {
					t.Fatalf("%s[%d,%d] = %d, want %d", variant, row, col, got[row*4+col], want)
				}
			}
		}
	}
}

func TestAccumulateAddsToExistingDestination(t *testing.T) {
	var a, b [32]byte
	for i := range a {
		a[i] = byte(i + 1)
		b[i] = byte(i*3 + 7)
	}
	var mul, acc [16]int32
	for i := range acc {
		acc[i] = int32(i + 10)
	}
	if err := ReferenceMul4x8(&mul, &a, &b, SignedSigned); err != nil {
		t.Fatal(err)
	}
	if err := ReferenceAccumulate4x8(&acc, &a, &b, SignedSigned); err != nil {
		t.Fatal(err)
	}
	for i := range acc {
		if want := mul[i] + int32(i+10); acc[i] != want {
			t.Fatalf("acc[%d] = %d, want %d", i, acc[i], want)
		}
	}
}

func TestRandomReferenceConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(0x6071))
	for _, variant := range Variants() {
		for n := 0; n < 200; n++ {
			var a, b [32]byte
			for i := range a {
				a[i] = byte(rng.Intn(256))
				b[i] = byte(rng.Intn(256))
			}
			var zero, acc [16]int32
			if err := ReferenceMul4x8(&zero, &a, &b, variant); err != nil {
				t.Fatal(err)
			}
			if err := ReferenceAccumulate4x8(&acc, &a, &b, variant); err != nil {
				t.Fatal(err)
			}
			if zero != acc {
				t.Fatalf("%s randomized mismatch: mul=%v acc=%v", variant, zero, acc)
			}
		}
	}
}

func TestReferenceMatrixMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x607160))
	const m, n, k = 8, 12, 16
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range a {
		a[i] = byte(rng.Intn(256))
	}
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	for _, variant := range Variants() {
		got := make([]int32, m*n)
		want := scalarMulMatrix(a, b, m, n, k, variant)
		if err := ReferenceMulMatrix(got, a, b, m, n, k, variant); err != nil {
			t.Fatal(err)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s matrix[%d] = %d, want %d", variant, i, got[i], want[i])
			}
		}
	}
}

func TestReferenceMatrixAccumulateAddsToExistingDestination(t *testing.T) {
	const m, n, k = 4, 8, 16
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range a {
		a[i] = byte(i*5 + 1)
	}
	for i := range b {
		b[i] = byte(i*7 + 3)
	}
	base := make([]int32, m*n)
	for i := range base {
		base[i] = int32(i + 100)
	}
	got := append([]int32(nil), base...)
	product := scalarMulMatrix(a, b, m, n, k, UnsignedSigned)
	if err := ReferenceAccumulateMatrix(got, a, b, m, n, k, UnsignedSigned); err != nil {
		t.Fatal(err)
	}
	for i := range got {
		if want := base[i] + product[i]; got[i] != want {
			t.Fatalf("accumulated[%d] = %d, want %d", i, got[i], want)
		}
	}
}

func TestReferenceMulMatrixClearsDestination(t *testing.T) {
	const m, n, k = 4, 4, 8
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range a {
		a[i] = 1
	}
	for i := range b {
		b[i] = 1
	}
	dst := make([]int32, m*n)
	for i := range dst {
		dst[i] = 1000
	}
	if err := ReferenceMulMatrix(dst, a, b, m, n, k, SignedSigned); err != nil {
		t.Fatal(err)
	}
	for i, got := range dst {
		if got != 8 {
			t.Fatalf("dst[%d] = %d, want 8", i, got)
		}
	}
}

func TestMulMatrixClearsDestinationBeforeUnavailable(t *testing.T) {
	if HasIME() && KernelAvailable() {
		t.Skip("real IME kernel available")
	}
	const m, n, k = 4, 4, 8
	dst := make([]int32, m*n)
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range dst {
		dst[i] = int32(i + 1)
	}
	err := MulMatrix(dst, a, b, m, n, k, SignedSigned)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("MulMatrix err = %v, want ErrUnavailable", err)
	}
	for i, got := range dst {
		if got != 0 {
			t.Fatalf("dst[%d] = %d, want zero after MulMatrix clear", i, got)
		}
	}
}

func TestAccumulateMatrixPreservesDestinationWhenUnavailable(t *testing.T) {
	if HasIME() && KernelAvailable() {
		t.Skip("real IME kernel available")
	}
	const m, n, k = 4, 4, 8
	dst := make([]int32, m*n)
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range dst {
		dst[i] = int32(i + 1)
	}
	before := append([]int32(nil), dst...)
	err := AccumulateMatrix(dst, a, b, m, n, k, SignedSigned)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("AccumulateMatrix err = %v, want ErrUnavailable", err)
	}
	for i := range dst {
		if dst[i] != before[i] {
			t.Fatalf("dst[%d] = %d, want preserved %d", i, dst[i], before[i])
		}
	}
}

func TestMatrixKernelMatchesReferenceOnX60(t *testing.T) {
	if !HasIME() || !KernelAvailable() {
		t.Skip("real IME kernel unavailable")
	}
	const m, n, k = 8, 8, 16
	a := make([]byte, m*k)
	b := make([]byte, n*k)
	for i := range a {
		a[i] = byte(i*5 + 11)
	}
	for i := range b {
		b[i] = byte(251 - i*7)
	}
	for _, variant := range Variants() {
		want := make([]int32, m*n)
		got := make([]int32, m*n)
		if err := ReferenceMulMatrix(want, a, b, m, n, k, variant); err != nil {
			t.Fatal(err)
		}
		if err := MulMatrix(got, a, b, m, n, k, variant); err != nil {
			t.Fatal(err)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s MulMatrix[%d] = %d, want %d", variant, i, got[i], want[i])
			}
		}

		accWant := make([]int32, m*n)
		accGot := make([]int32, m*n)
		for i := range accWant {
			accWant[i] = int32(i + 17)
			accGot[i] = int32(i + 17)
		}
		if err := ReferenceAccumulateMatrix(accWant, a, b, m, n, k, variant); err != nil {
			t.Fatal(err)
		}
		if err := AccumulateMatrix(accGot, a, b, m, n, k, variant); err != nil {
			t.Fatal(err)
		}
		for i := range accGot {
			if accGot[i] != accWant[i] {
				t.Fatalf("%s AccumulateMatrix[%d] = %d, want %d", variant, i, accGot[i], accWant[i])
			}
		}
	}
}

func TestMatrixInvalidShape(t *testing.T) {
	tests := []struct {
		name string
		dst  []int32
		a    []byte
		b    []byte
		m    int
		n    int
		k    int
	}{
		{name: "zero m", dst: make([]int32, 16), a: make([]byte, 32), b: make([]byte, 32), m: 0, n: 4, k: 8},
		{name: "m not multiple", dst: make([]int32, 20), a: make([]byte, 40), b: make([]byte, 32), m: 5, n: 4, k: 8},
		{name: "n not multiple", dst: make([]int32, 20), a: make([]byte, 32), b: make([]byte, 40), m: 4, n: 5, k: 8},
		{name: "k not multiple", dst: make([]int32, 16), a: make([]byte, 36), b: make([]byte, 36), m: 4, n: 4, k: 9},
		{name: "short dst", dst: make([]int32, 15), a: make([]byte, 32), b: make([]byte, 32), m: 4, n: 4, k: 8},
		{name: "short a", dst: make([]int32, 16), a: make([]byte, 31), b: make([]byte, 32), m: 4, n: 4, k: 8},
		{name: "short b", dst: make([]int32, 16), a: make([]byte, 32), b: make([]byte, 31), m: 4, n: 4, k: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ReferenceMulMatrix(tt.dst, tt.a, tt.b, tt.m, tt.n, tt.k, SignedSigned)
			if !errors.Is(err, ErrInvalidShape) {
				t.Fatalf("ReferenceMulMatrix err = %v, want ErrInvalidShape", err)
			}
			err = ReferenceAccumulateMatrix(tt.dst, tt.a, tt.b, tt.m, tt.n, tt.k, SignedSigned)
			if !errors.Is(err, ErrInvalidShape) {
				t.Fatalf("ReferenceAccumulateMatrix err = %v, want ErrInvalidShape", err)
			}
			err = MulMatrix(tt.dst, tt.a, tt.b, tt.m, tt.n, tt.k, SignedSigned)
			if !errors.Is(err, ErrInvalidShape) {
				t.Fatalf("MulMatrix err = %v, want ErrInvalidShape", err)
			}
			err = AccumulateMatrix(tt.dst, tt.a, tt.b, tt.m, tt.n, tt.k, SignedSigned)
			if !errors.Is(err, ErrInvalidShape) {
				t.Fatalf("AccumulateMatrix err = %v, want ErrInvalidShape", err)
			}
		})
	}
}

func TestMulUnavailableOffX60(t *testing.T) {
	if HasIME() && KernelAvailable() {
		t.Skip("real IME kernel available")
	}
	var dst [16]int32
	var a, b [32]byte
	err := Mul4x8(&dst, &a, &b, SignedSigned)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Mul4x8 err = %v, want ErrUnavailable", err)
	}
}

func scalarMulMatrix(a, b []byte, m, n, k int, variant Variant) []int32 {
	dst := make([]int32, m*n)
	for row := 0; row < m; row++ {
		for col := 0; col < n; col++ {
			for kk := 0; kk < k; kk++ {
				dst[row*n+col] += scalarWidenA(a[row*k+kk], variant) * scalarWidenB(b[col*k+kk], variant)
			}
		}
	}
	return dst
}

func scalarWidenA(x byte, variant Variant) int32 {
	switch variant {
	case UnsignedUnsigned, UnsignedSigned:
		return int32(x)
	default:
		if x >= 128 {
			return int32(x) - 256
		}
		return int32(x)
	}
}

func scalarWidenB(x byte, variant Variant) int32 {
	switch variant {
	case UnsignedUnsigned, SignedUnsigned:
		return int32(x)
	default:
		if x >= 128 {
			return int32(x) - 256
		}
		return int32(x)
	}
}
