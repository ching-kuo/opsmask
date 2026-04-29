# Kubernetes masking reference

Use this reference when handling Kubernetes metadata, especially `kubectl get`
output, pod follow-ups, namespace context, or YAML/JSON field selection.

## Prefer mask-friendly field output

Prefer read-only commands that print one concise record per line and keep
resource nouns next to resource names. This makes masking more reliable than raw
tables or full YAML dumps.

Good shapes:

```sh
kubectl get pods -n <namespace> -o name | llm-mask mask --summary

kubectl get pods -A -o go-template='{{range .items}}{{printf "namespace/%s pod/%s node/%s phase=%s restarts=" .metadata.namespace .metadata.name .spec.nodeName .status.phase}}{{range .status.containerStatuses}}{{printf "%d," .restartCount}}{{end}}{{printf "\n"}}{{end}}' \
  | llm-mask mask --summary

kubectl get pods -A -o json \
  | jq -r '.items[] | "namespace/\(.metadata.namespace) pod/\(.metadata.name) node/\(.spec.nodeName // "none") phase=\(.status.phase) restarts=\([.status.containerStatuses[]?.restartCount] | join(","))"' \
  | llm-mask mask --summary
```

Why this shape works:

- `namespace/<value>` is detected as `k8snamespace`.
- `pod/<value>` is detected as `k8spod`.
- `node/<value>` is detected as `k8snode`.
- Other fields such as `phase=Running` and `restarts=0` remain readable because
  they are operational state, not identifiers.

Avoid raw `kubectl get pods -A` table output when possible: namespace and pod
names appear as separate bare columns, so built-in masking may not identify every
value. Avoid full `-o yaml` unless required; YAML often includes labels,
annotations, image references, service account names, env metadata, and other
identifying context.

## Response format for masked Kubernetes metadata

When responding:

- Preserve all sentinel tokens exactly as provided.
- Refer to resources by sentinel, not by guessed real names.
- Describe operational state in plain language: phase, restart count, readiness,
  age, status reason, event reason, etc.
- Keep namespace context with pod follow-ups. If masked input came from all
  namespaces, include `-n <masked namespace token>` with the masked pod token.
  When a `namespace/<name>` or `ns/<name>` sentinel is used as a `kubectl -n` or
  `--namespace` value, `llm-mask exec` resolves it locally and passes only the
  bare namespace name to `kubectl`.
- If suggesting shell commands with ASCII sentinels, quote the sentinel or use
  `noglob` in zsh.

Example follow-up command:

```sh
llm-mask exec -- kubectl describe -n '[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]]' '[[llm-mask:k8spod:0123456789abcdef]]'
```

zsh-safe alternative:

```sh
noglob llm-mask exec -- kubectl describe -n [[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]] [[llm-mask:k8spod:0123456789abcdef]]
```

## Common failure: NotFound after masking

If a command like this returns masked NotFound:

```sh
llm-mask exec -- kubectl describe '[[llm-mask:k8spod:0123456789abcdef]]'
```

the token may have resolved correctly, but `kubectl` searched the current
namespace. Retry with the namespace:

```sh
llm-mask exec -- kubectl describe -n '[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]]' '[[llm-mask:k8spod:0123456789abcdef]]'
```

`kubectl describe -A pod/<name>` is not a replacement; Kubernetes cannot
retrieve a named resource across all namespaces without a namespace.
