#!/usr/bin/env bash
# Run manually against the current kubectl context; requires kubectl, go, GNU timeout and tail.
set -euo pipefail

for tool in kubectl go timeout tail; do
  command -v "$tool" >/dev/null || { echo "Missing $tool" >&2; exit 1; }
done

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
namespace="kube-health-e2e-$(date +%s)-$RANDOM"
logs=$(mktemp -d)
created=false
pids=()
tail_pids=()
cleanup() {
  for pid in "${tail_pids[@]}" "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  if [[ $created == true ]]; then
    kubectl delete namespace "$namespace" --wait=false --ignore-not-found >/dev/null || true
  fi
  rm -rf "$logs"
}
trap cleanup EXIT

printf 'Using kubectl context: %s\n' "$(kubectl config current-context)"
kubectl create namespace "$namespace"
created=true

# Start waiting before either named resource exists. The first request also
# lists ConfigMaps (a type-only query); it must still wait for the named
# Deployment. Do not create them until both CLIs report Unknown and remain running.
timeout 180s go run ./main.go -n "$namespace" --wait-ok -o tree deployment/e2e-deployment configmap >"$logs/deployment" 2>&1 &
deployment_pid=$!
pids+=("$deployment_pid")
timeout 180s go run ./main.go -n "$namespace" --wait-ok -o tree pod/e2e-pod >"$logs/pod" 2>&1 &
pod_pid=$!
pids+=("$pod_pid")

# Stream output while retaining the logs for the assertions below.
tail -n +1 -f --pid="$deployment_pid" "$logs/deployment" &
tail_pids+=("$!")
tail -n +1 -f --pid="$pod_pid" "$logs/pod" &
tail_pids+=("$!")

wait_for_missing() {
  local pid=$1 file=$2 expected=$3
  local deadline=$((SECONDS + 90))
  while ! grep -Fq "Unknown $namespace/$expected" "$file"; do
    if ! kill -0 "$pid" 2>/dev/null || (( SECONDS >= deadline )); then
      echo "Did not observe missing $expected while waiting:" >&2
      cat "$file" >&2
      return 1
    fi
    sleep 1
  done
  kill -0 "$pid" 2>/dev/null || { echo "Wait for $expected exited too early" >&2; return 1; }
}
wait_for_missing "$deployment_pid" "$logs/deployment" Deployment/e2e-deployment
wait_for_missing "$pod_pid" "$logs/pod" Pod/e2e-pod

kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: e2e-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: e2e-deployment
  template:
    metadata:
      labels:
        app: e2e-deployment
    spec:
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: agnhost
          image: registry.k8s.io/e2e-test-images/agnhost:2.45
          command: ["/agnhost", "netexec", "--http-port=8080"]
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
          ports:
            - containerPort: 8080
          readinessProbe:
            httpGet:
              path: /hostname
              port: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: e2e-pod
spec:
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: agnhost
      image: registry.k8s.io/e2e-test-images/agnhost:2.45
      command: ["/agnhost", "netexec", "--http-port=8080"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
      ports:
        - containerPort: 8080
      readinessProbe:
        httpGet:
          path: /hostname
          port: 8080
YAML

# Both original waiters must exit successfully after the objects appear and become healthy.
for entry in "deployment:$deployment_pid:Deployment/e2e-deployment" "pod:$pod_pid:Pod/e2e-pod"; do
  IFS=: read -r file pid expected <<<"$entry"
  if ! wait "$pid" || ! grep -Fq "Ok $namespace/$expected" "$logs/$file"; then
    echo "Wait for $expected failed:" >&2
    cat "$logs/$file" >&2
    exit 1
  fi
done
pids=()
for pid in "${tail_pids[@]}"; do
  wait "$pid"
done
tail_pids=()
echo 'E2E deployment and pod appeared and became healthy.'
