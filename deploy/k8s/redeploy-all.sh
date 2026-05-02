#!/usr/bin/env bash
# Redeploy all 3 per-network unified explorer pods on lux-k8s.
#
# Prerequisite: doctl auth init && kubectl config use-context do-sfo3-lux-k8s
# Image:        ghcr.io/luxfi/indexer:main (built from this commit)
# Network ns:   lux-mainnet, lux-testnet, lux-devnet
#
# This script is idempotent. Run it after a new image is published; existing
# PVCs are reused so per-chain SQLite state survives a pod restart.
set -euo pipefail

CONTEXT=${KUBE_CONTEXT:-do-sfo3-lux-k8s}
MANIFEST_DIR=${MANIFEST_DIR:-$(cd "$(dirname "$0")/../../../universe/k8s/lux-explorer" && pwd)}
IMAGE=${IMAGE:-ghcr.io/luxfi/indexer:main}

echo "==> Context:   $CONTEXT"
echo "==> Manifests: $MANIFEST_DIR"
echo "==> Image:     $IMAGE"

kubectl --context "$CONTEXT" config use-context "$CONTEXT" >/dev/null

# Apply each network's deployment.
for net in mainnet testnet devnet; do
  echo
  echo "==> [${net}] Apply manifest"
  kubectl --context "$CONTEXT" apply -f "$MANIFEST_DIR/explorer-${net}-v2.yaml"

  # Pin the image (in case the manifest hasn't been bumped) and force a
  # rolling restart so the pod picks up the freshly built image.
  kubectl --context "$CONTEXT" -n lux-explorer set image \
    deployment/explorer-${net}-v2 explorer="$IMAGE"
  kubectl --context "$CONTEXT" -n lux-explorer rollout restart \
    deployment/explorer-${net}-v2

  echo "==> [${net}] Waiting for rollout"
  kubectl --context "$CONTEXT" -n lux-explorer rollout status \
    deployment/explorer-${net}-v2 --timeout=180s
done

# Update IngressRoutes to route per-network hostnames at the per-network
# service. The mainnet host stays on the mainnet pod; testnet/devnet hosts
# move off the shared mainnet pod onto their own.
cat <<'YAML' | kubectl --context "$CONTEXT" apply -f -
---
apiVersion: hanzo.ai/v1alpha1
kind: IngressRoute
metadata:
  name: lux-explorer-testnet
  namespace: lux-explorer
  labels: { app: lux-explorer, network: testnet }
spec:
  entryPoints: [websecure]
  tls:
    certResolver: letsencrypt
  routes:
  - kind: Rule
    match: Host(`explore-test.lux.network`) || Host(`explore.lux-test.network`)
    services:
    - { name: lux-frontend-testnet-proxy, port: 3000 }
  - kind: Rule
    match: Host(`api-explore-test.lux.network`) || Host(`api-explore.lux-test.network`)
    middlewares:
    - { name: mainpage-rewrite }
    - { name: blockscout-prefix }
    - { name: cors-allow-all }
    services:
    - { name: explorer-testnet-v2, port: 8090 }
---
apiVersion: hanzo.ai/v1alpha1
kind: IngressRoute
metadata:
  name: lux-explorer-devnet
  namespace: lux-explorer
  labels: { app: lux-explorer, network: devnet }
spec:
  entryPoints: [websecure]
  tls:
    certResolver: letsencrypt
  routes:
  - kind: Rule
    match: Host(`explore-dev.lux.network`) || Host(`explore.lux-dev.network`)
    services:
    - { name: lux-frontend-devnet-proxy, port: 3000 }
  - kind: Rule
    match: Host(`api-explore-dev.lux.network`) || Host(`api-explore.lux-dev.network`)
    middlewares:
    - { name: mainpage-rewrite }
    - { name: blockscout-prefix }
    - { name: cors-allow-all }
    services:
    - { name: explorer-devnet-v2, port: 8090 }
YAML

# Update mainnet IRs (in lux-mainnet ns) to point at the network-specific
# service rather than the legacy lux-explorer/explorer pod.
for chain in mainnet zoo hanzo spc pars; do
  echo "==> [mainnet/$chain] patch IR to use explorer-mainnet-v2"
  kubectl --context "$CONTEXT" -n lux-mainnet \
    patch ingressroute "lux-explorer-${chain}" --type=json -p="$(cat <<JSON
[
  {"op":"test","path":"/spec/routes/0/services/0/name","value":"explorer-unified"},
  {"op":"replace","path":"/spec/routes/0/services/0/name","value":"explorer-mainnet-v2-proxy"}
]
JSON
)" 2>/dev/null || true
done

# ExternalName service so lux-mainnet IRs can reach the per-network service in
# lux-explorer ns.
cat <<'YAML' | kubectl --context "$CONTEXT" apply -f -
---
apiVersion: v1
kind: Service
metadata:
  name: explorer-mainnet-v2-proxy
  namespace: lux-mainnet
spec:
  type: ExternalName
  externalName: explorer-mainnet-v2.lux-explorer.svc.cluster.local
  ports:
  - { name: http, port: 8090, targetPort: 8090, protocol: TCP }
YAML

echo
echo "==> Verifying live endpoints"
for d in api-explore.lux.network api-explore.zoo.network api-explore.hanzo.network \
         api-explore-spc.lux.network api-explore.pars.network \
         api-explore-test.lux.network api-explore.lux-test.network \
         api-explore-dev.lux.network api-explore.lux-dev.network; do
  blocks=$(curl -sk -m 5 "https://$d/stats" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('total_blocks','?'))" 2>/dev/null || echo "?")
  printf "  %-32s blocks=%s\n" "$d" "$blocks"
done

echo
echo "==> Done. Per-chain DBs at /data/{slug}/query/indexer.db inside each pod."
