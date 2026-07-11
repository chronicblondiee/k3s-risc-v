# Node benchmark history

**Board:** Orange Pi RV2 (`k8s-rv2-01`, riscv64, SpacemiT X60 8-core, NVMe root fs)
**Playbook:** `playbooks/11_riscv64_node_benchmark.yml`

Summary of every full benchmark run, newest first. Rows are appended
automatically by the playbook (full runs only — `--tags` subsets fetch a
report but don't add a row here). Full raw tool output lives in the
linked reports under `benchmarks/results/`.

All numbers are "node as operated": k3s was running during every run, so
its background CPU/I-O load is part of the results (the linked report
records the pod count). Storage tests use direct I/O (page cache
bypassed); host and PVC tests hit the same NVMe — the PVC columns show
the k8s local-path volume-path overhead, not a different disk.

Columns: sysbench CPU events/sec (single thread and all cores), sysbench
memory bandwidth, fio sequential bandwidth (1M blocks, iodepth 8), fio
4k random bandwidth (iodepth 32), the same fio tests through a
local-path PVC, and iperf3 bitrates (node loopback / LAN receive / LAN
transmit / host->pod through the CNI; LAN cells are n/a when the
control machine has no iperf3).

## Runs

| Run | k3s | Kernel | CPU 1T (ev/s) | CPU all (ev/s) | Mem W/R (MiB/s) | NVMe seq W/R | NVMe 4k rand W/R | PVC seq W / 4k rand R | Net loop / LAN rx / LAN tx / pod | Report |
|---|---|---|---|---|---|---|---|---|---|---|
<!-- newest-first: playbook 11 inserts summary rows below this line -->
| 20260711T111053 | v1.36.2+k3s1 | 6.18.35-current-spacemit | 790.70 | 6089.57 | 6785.95 / 14323.97 | 772MiB/s / 818MiB/s | 164MiB/s / 137MiB/s | 771MiB/s / 145MiB/s | 4.13 Gbits/sec / 743 Mbits/sec / 821 Mbits/sec / 3.91 Gbits/sec | [report](../benchmarks/results/k8s-rv2-01-20260711T111053.md) |
| 20260711T105642 | v1.36.2+k3s1 | 6.18.35-current-spacemit | 791.16 | 6083.84 | 6551.75 / 13851.20 | 773MiB/s / 818MiB/s | 170MiB/s / 141MiB/s | 772MiB/s / 112MiB/s | n/a | [report](../benchmarks/results/k8s-rv2-01-20260711T105642.md) |
| 20260711T104227 | v1.36.2+k3s1 | 6.18.35-current-spacemit | 790.78 | 6087.41 | 6667.65 / 13197.13 | 772MiB/s / 819MiB/s | 160MiB/s / 195MiB/s | 773MiB/s / 153MiB/s | n/a | [report](../benchmarks/results/k8s-rv2-01-20260711T104227.md) |

Note on the 20260711T104227 row: sysbench ran with `--threads=16` (an
`ansible_processor_vcpus` fact bug on the X60's odd cpuinfo topology —
it reports 16 on an 8-core no-SMT part). Later runs use 8 threads via
`ansible_processor_nproc`; the all-core CPU number is comparable anyway
(scheduler-bound either way), but it's recorded here for honesty.
