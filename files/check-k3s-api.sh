#!/bin/sh
set -eu

/usr/local/bin/k3s kubectl \
  --kubeconfig=/etc/rancher/k3s/k3s.yaml \
  --request-timeout=2s \
  get --raw=/readyz | grep -qx ok
