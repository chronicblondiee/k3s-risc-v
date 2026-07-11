package ime

import (
	"errors"
	"os"
	"strings"
	"sync"
)

var ErrUnavailable = errors.New("spacemit x60 ime unavailable")

var (
	hasIMEOnce   sync.Once
	hasIMECached bool
)

type Variant int

const (
	SignedSigned Variant = iota
	UnsignedUnsigned
	SignedUnsigned
	UnsignedSigned
)

func (v Variant) String() string {
	switch v {
	case SignedSigned:
		return "signed-signed"
	case UnsignedUnsigned:
		return "unsigned-unsigned"
	case SignedUnsigned:
		return "signed-unsigned"
	case UnsignedSigned:
		return "unsigned-signed"
	default:
		return "unknown"
	}
}

func Variants() []Variant {
	return []Variant{SignedSigned, UnsignedUnsigned, SignedUnsigned, UnsignedSigned}
}

func HasIME() bool {
	hasIMEOnce.Do(func() {
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			hasIMECached = hasIMECPUInfo(string(data))
		}
	})
	return hasIMECached
}

func KernelAvailable() bool {
	return kernelAvailable
}

func Mul4x8(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	if err := validVariant(variant); err != nil {
		return err
	}
	clear(dst[:])
	return Accumulate4x8(dst, a, b, variant)
}

func Accumulate4x8(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	if err := validVariant(variant); err != nil {
		return err
	}
	if !HasIME() || !kernelAvailable {
		return ErrUnavailable
	}
	return runKernel(dst, a, b, variant)
}

func ReferenceMul4x8(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	if err := validVariant(variant); err != nil {
		return err
	}
	clear(dst[:])
	return ReferenceAccumulate4x8(dst, a, b, variant)
}

func ReferenceAccumulate4x8(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	if err := validVariant(variant); err != nil {
		return err
	}
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			var sum int32
			for k := 0; k < 8; k++ {
				av := widenA(a[row*8+k], variant)
				bv := widenB(b[col*8+k], variant)
				sum += av * bv
			}
			dst[row*4+col] += sum
		}
	}
	return nil
}

func hasIMECPUInfo(cpuinfo string) bool {
	info := strings.ToLower(cpuinfo)
	return strings.Contains(info, "uarch") && strings.Contains(info, "spacemit,x60") ||
		strings.Contains(info, "mvendorid") && strings.Contains(info, "0x710")
}

func validVariant(variant Variant) error {
	switch variant {
	case SignedSigned, UnsignedUnsigned, SignedUnsigned, UnsignedSigned:
		return nil
	default:
		return errors.New("unknown ime variant")
	}
}

func widenA(x byte, variant Variant) int32 {
	switch variant {
	case UnsignedUnsigned, UnsignedSigned:
		return int32(x)
	default:
		return int32(int8(x))
	}
}

func widenB(x byte, variant Variant) int32 {
	switch variant {
	case UnsignedUnsigned, SignedUnsigned:
		return int32(x)
	default:
		return int32(int8(x))
	}
}
