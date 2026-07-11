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
