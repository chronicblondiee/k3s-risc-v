package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/chronicblondiee/k3s-risc-v/tools/x60-ime-go/ime"
)

const (
	probeEnvKey   = "IMEBENCH_PROBE_CHILD"
	probeEnvValue = "1"
)

type benchReport struct {
	Timestamp       string        `json:"timestamp"`
	Kernel          string        `json:"kernel"`
	GoVersion       string        `json:"go_version"`
	GOOS            string        `json:"goos"`
	GOARCH          string        `json:"goarch"`
	KernelAvailable bool          `json:"kernel_available"`
	HasIME          bool          `json:"has_ime"`
	CPUInfoGate     string        `json:"cpuinfo_gate"`
	Iterations      int           `json:"iterations"`
	Results         []benchResult `json:"results"`
}

type benchResult struct {
	Variant        string  `json:"variant"`
	Path           string  `json:"path"`
	Iterations     int     `json:"iterations"`
	TotalNanos     int64   `json:"total_nanos"`
	NanosPerTile   float64 `json:"nanos_per_tile"`
	TilesPerSecond float64 `json:"tiles_per_second"`
	Skipped        bool    `json:"skipped"`
	SkipReason     string  `json:"skip_reason,omitempty"`
}

func main() {
	if os.Getenv(probeEnvKey) == probeEnvValue {
		os.Exit(runProbeChild())
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "detect":
		if err := detect(); err != nil {
			fatal(err)
		}
	case "selftest":
		if err := selftest(); err != nil {
			fatal(err)
		}
	case "bench":
		if err := bench(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: imebench <detect|selftest|bench>\n")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "imebench: %v\n", err)
	os.Exit(1)
}

func detect() error {
	fmt.Printf("go: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("kernel_available: %v\n", ime.KernelAvailable())
	fmt.Printf("has_ime: %v\n", ime.HasIME())
	fmt.Printf("cpuinfo_gate: %s\n", cpuInfoGate())
	return nil
}

func selftest() error {
	if err := detect(); err != nil {
		return err
	}
	if ime.HasIME() && ime.KernelAvailable() {
		if err := runProbeParent(); err != nil {
			return err
		}
		fmt.Println("sigill_probe: ok")
	} else {
		fmt.Println("sigill_probe: skipped (IME unavailable)")
	}
	if err := runCorrectness(); err != nil {
		return err
	}
	fmt.Println("correctness: ok")
	return nil
}

func runProbeParent() error {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), probeEnvKey+"="+probeEnvValue)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("IME child probe failed: %w%s", err, formatStderr(stderr.String()))
	}
	return nil
}

func runProbeChild() int {
	var dst [16]int32
	var a, b [32]byte
	for i := range a {
		a[i] = byte(i)
		b[i] = byte(31 - i)
	}
	if err := ime.Mul4x8(&dst, &a, &b, ime.SignedSigned); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runCorrectness() error {
	cases := edgeCases()
	rng := rand.New(rand.NewSource(0x6071))
	for i := 0; i < 200; i++ {
		var a, b [32]byte
		for j := range a {
			a[j] = byte(rng.Intn(256))
			b[j] = byte(rng.Intn(256))
		}
		cases = append(cases, [2][32]byte{a, b})
	}
	for _, variant := range ime.Variants() {
		for i, tc := range cases {
			var want, got [16]int32
			if err := ime.ReferenceMul4x8(&want, &tc[0], &tc[1], variant); err != nil {
				return err
			}
			err := ime.Mul4x8(&got, &tc[0], &tc[1], variant)
			if errors.Is(err, ime.ErrUnavailable) {
				continue
			}
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("%s case %d mismatch: got %v want %v", variant, i, got, want)
			}
		}
	}
	return nil
}

func edgeCases() [][2][32]byte {
	var zeros, ones, high, ramp, inverse [32]byte
	for i := range zeros {
		ones[i] = 1
		high[i] = 0xff
		ramp[i] = byte(i)
		inverse[i] = byte(255 - i)
	}
	return [][2][32]byte{
		{zeros, zeros},
		{ones, ones},
		{high, ones},
		{ramp, inverse},
		{inverse, ramp},
	}
}

func bench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	iterations := fs.Int("iterations", 200000, "tile iterations per variant/path")
	mdPath := fs.String("markdown", "", "write Markdown report to path")
	jsonPath := fs.String("json", "", "write JSON report to path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iterations <= 0 {
		return errors.New("iterations must be positive")
	}
	report := benchReport{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Kernel:          uname(),
		GoVersion:       runtime.Version(),
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		KernelAvailable: ime.KernelAvailable(),
		HasIME:          ime.HasIME(),
		CPUInfoGate:     cpuInfoGate(),
		Iterations:      *iterations,
	}
	for _, variant := range ime.Variants() {
		report.Results = append(report.Results, runOneBench("pure-go", variant, *iterations))
		report.Results = append(report.Results, runOneBench("ime", variant, *iterations))
	}
	md := renderMarkdown(report)
	js, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if *mdPath != "" {
		if err := os.WriteFile(*mdPath, []byte(md), 0o644); err != nil {
			return err
		}
	}
	if *jsonPath != "" {
		if err := os.WriteFile(*jsonPath, append(js, '\n'), 0o644); err != nil {
			return err
		}
	}
	fmt.Print(md)
	return nil
}

func runOneBench(path string, variant ime.Variant, iterations int) benchResult {
	var dst [16]int32
	var a, b [32]byte
	for i := range a {
		a[i] = byte(i*7 + 3)
		b[i] = byte(251 - i*5)
	}
	start := time.Now()
	var err error
	for i := 0; i < iterations; i++ {
		if path == "pure-go" {
			err = ime.ReferenceMul4x8(&dst, &a, &b, variant)
		} else {
			err = ime.Mul4x8(&dst, &a, &b, variant)
		}
		if errors.Is(err, ime.ErrUnavailable) {
			return benchResult{
				Variant:    variant.String(),
				Path:       path,
				Iterations: iterations,
				Skipped:    true,
				SkipReason: err.Error(),
			}
		}
		if err != nil {
			return benchResult{
				Variant:    variant.String(),
				Path:       path,
				Iterations: iterations,
				Skipped:    true,
				SkipReason: err.Error(),
			}
		}
	}
	elapsed := time.Since(start)
	nsPerTile := float64(elapsed.Nanoseconds()) / float64(iterations)
	return benchResult{
		Variant:        variant.String(),
		Path:           path,
		Iterations:     iterations,
		TotalNanos:     elapsed.Nanoseconds(),
		NanosPerTile:   nsPerTile,
		TilesPerSecond: 1e9 / nsPerTile,
	}
}

func renderMarkdown(report benchReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# SpacemiT X60 IME Go benchmark\n\n")
	fmt.Fprintf(&b, "- Timestamp: `%s`\n", report.Timestamp)
	fmt.Fprintf(&b, "- Kernel: `%s`\n", report.Kernel)
	fmt.Fprintf(&b, "- Go: `%s` `%s/%s`\n", report.GoVersion, report.GOOS, report.GOARCH)
	fmt.Fprintf(&b, "- CPU gate: `%s`\n", report.CPUInfoGate)
	fmt.Fprintf(&b, "- Has IME: `%v`\n", report.HasIME)
	fmt.Fprintf(&b, "- cgo kernel compiled: `%v`\n", report.KernelAvailable)
	fmt.Fprintf(&b, "- Iterations per row: `%d`\n\n", report.Iterations)
	fmt.Fprintf(&b, "| Variant | Path | Iterations | ns/tile | tiles/sec | Status |\n")
	fmt.Fprintf(&b, "| --- | --- | ---: | ---: | ---: | --- |\n")
	for _, r := range report.Results {
		if r.Skipped {
			fmt.Fprintf(&b, "| %s | %s | %d | - | - | skipped: %s |\n", r.Variant, r.Path, r.Iterations, r.SkipReason)
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %.2f | %.2f | ok |\n",
			r.Variant, r.Path, r.Iterations, r.NanosPerTile, r.TilesPerSecond)
	}
	return b.String()
}

func cpuInfoGate() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unavailable"
	}
	info := strings.ToLower(string(data))
	var hits []string
	if strings.Contains(info, "uarch") && strings.Contains(info, "spacemit,x60") {
		hits = append(hits, "uarch: spacemit,x60")
	}
	if strings.Contains(info, "mvendorid") && strings.Contains(info, "0x710") {
		hits = append(hits, "mvendorid: 0x710")
	}
	if len(hits) == 0 {
		return "no x60 marker"
	}
	return strings.Join(hits, ", ")
}

func uname() string {
	out, err := exec.Command("uname", "-srvm").Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}

func formatStderr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return ": " + s
}
