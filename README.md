# k8s-arm

Ansible-managed home Kubernetes lab spanning two architectures: an arm64
board (Orange Pi 5 Plus, RK3588) and a riscv64 board (Orange Pi RV2), both
running Armbian. Target distribution is [k3s](https://k3s.io/) across the
whole cluster — chosen because it's a single static binary and, unlike
microk8s, has a real (if from-source) path to a riscv64 build.

Started arm64-only (repo name predates the RISC-V addition); the two nodes
are run and validated independently, not yet joined into a single
multi-node cluster. **The arm64 board is currently offline (hardware
failure)** — active focus is riscv64-only for now. Future work: repair or
replace the arm64 node and add a new x86 node, then resume the
mixed-architecture cluster goal. See `AGENTS.md` for full node-by-node
detail, hardware notes, and known gotchas, and `docs/` for narrative
runbooks of specific incidents and builds (an NVMe-install brick and
recovery, the riscv64 from-source k3s build, and a local riscv64 OCI
registry).

## Repo layout

```
ansible.cfg, requirements.yml                  - Ansible project config
inventory.ini.example                          - copy to inventory.ini (gitignored) with your own host/IP/user
group_vars/all/vault.yml.example               - copy to vault.yml (gitignored), ansible-vault encrypt
host_vars/<hostname>.yml.example                - copy to <hostname>.yml (gitignored) with your own static IP/gateway
playbooks/00-04_*.yml                          - bootstrap -> NVMe install -> base config -> SSH hardening -> node prep
playbooks/05_k3s_riscv64_build.yml             - build+install k3s from source on the riscv64 node
playbooks/06_riscv64_registry.yml              - local OCI registry for riscv64 image distribution
templates/                                     - Jinja2 templates rendered by the playbooks above
files/                                         - static assets (e.g. the hand-built riscv64 pause image source)
tools/                                         - hardware-recovery build scripts (RK3588 Maskrom recovery)
docs/                                          - incident logs / troubleshooting runbooks
```

## Prerequisites

- Ansible (`ansible-playbook`, `ansible-vault`) on your control machine.
- SSH access to your own boards, with a key already deployed (see
  `playbooks/00_bootstrap_keys.yml` for the first-run bootstrap).
- `gh` (GitHub CLI) is only needed if you're forking this to manage your own
  copy the same way; not required to just run the playbooks.

## Quickstart

None of `inventory.ini`, `host_vars/*.yml`, or `group_vars/all/vault.yml`
are committed — they hold your real hosts, IPs, and credentials. Set them up
from the provided templates:

```bash
cp inventory.ini.example inventory.ini
cp host_vars/k8s-node-01.yml.example host_vars/k8s-node-01.yml
cp host_vars/k8s-rv2-01.yml.example host_vars/k8s-rv2-01.yml
cp group_vars/all/vault.yml.example group_vars/all/vault.yml
# edit all of the above with your real hostnames/IPs/usernames, then:
ansible-vault encrypt group_vars/all/vault.yml
echo 'your-chosen-vault-password' > .vault_pass && chmod 600 .vault_pass
```

Then run the playbooks in order (`00` through `06`, skipping any that don't
apply to your hardware) — see `AGENTS.md` for what each one does and the
node-specific gotchas encountered along the way.

## License

[MIT](LICENSE)
