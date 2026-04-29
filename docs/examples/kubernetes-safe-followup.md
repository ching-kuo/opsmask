# Kubernetes real-case workflow example (non-identifiable)

This example shows how to use `llm-mask` in a real Kubernetes investigation
without putting real cluster identifiers into the repository. The workflow is:
collect only the minimum read-only data needed, mask it before analysis, preserve
sentinel tokens verbatim, and run any follow-up lookup through `llm-mask exec`.
Never run `llm-mask unmask` from an agent session.

## 1. Capture metadata in a mask-friendly shape

Prefer Kubernetes output forms that keep the resource noun next to the name,
such as `-o name`. That shape lets the built-in Kubernetes detector recognize
resource references reliably:

```sh
kubectl get pods -A -o name \
  | llm-mask mask --summary --ascii-tokens > masked-pods.txt
```

Representative masked output:

```text
[[llm-mask:k8spod:1111111111111111]]
[[llm-mask:k8spod:2222222222222222]]
[[llm-mask:k8spod:3333333333333333]]
```

Representative summary:

```text
masked=3 destroyed=0 k8spod=3
```

This shape was smoke-tested against a real cluster with output suppressed; the
first 25 pod resource lines produced:

```text
masked=25 destroyed=0 k8spod=25
```

## 2. Capture selected pod fields instead of full YAML

When you need more context than `pod/<name>`, prefer a field-selected output
that explicitly labels identifiers. This is easier to mask than full YAML and
easier for an LLM to reason over than the default table:

```sh
kubectl get pods -A -o go-template='{{range .items}}{{printf "namespace/%s pod/%s node/%s phase=%s restarts=" .metadata.namespace .metadata.name .spec.nodeName .status.phase}}{{range .status.containerStatuses}}{{printf "%d," .restartCount}}{{end}}{{printf "\n"}}{{end}}' \
  | llm-mask mask --summary --ascii-tokens > masked-pod-fields.txt
```

Representative masked output:

```text
[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]] [[llm-mask:k8spod:bbbbbbbbbbbbbbbb]] [[llm-mask:k8snode:cccccccccccccccc]] phase=Running restarts=0,
[[llm-mask:k8snamespace:dddddddddddddddd]] [[llm-mask:k8spod:eeeeeeeeeeeeeeee]] [[llm-mask:k8snode:ffffffffffffffff]] phase=Pending restarts=0,
```

The plain fields remain readable (`phase`, `restarts`) while namespace, pod, and
node values become sentinels. This shape was also smoke-tested against a real
cluster with output suppressed; the first 25 lines produced:

```text
masked=75 destroyed=0 k8snamespace=25 k8snode=25 k8spod=25
```

Avoid pasting raw `kubectl get pods -A` table output into an agent. In table
form, namespace and pod names appear as separate columns without `pod/` prefixes;
if your project needs table-form output, add trusted project regex rules for the
specific columns or names you want pseudonymized.

If you do collect from all namespaces, keep the namespace context locally.
`kubectl get pods -A -o name` emits `pod/<name>` values without the namespace,
so a later `kubectl describe '[[llm-mask:k8spod:...]]'` will look in the
current namespace and may return a masked NotFound error even though the token
resolved correctly.

## 3. Ask the agent to reason over masked text only

Safe prompt shape:

```text
Here is masked Kubernetes metadata. Preserve all llm-mask sentinels verbatim.
Which pod should I inspect next, and what namespace-qualified read-only command
should I run?

[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]] [[llm-mask:k8spod:bbbbbbbbbbbbbbbb]] [[llm-mask:k8snode:cccccccccccccccc]] phase=Running restarts=0,
[[llm-mask:k8snamespace:dddddddddddddddd]] [[llm-mask:k8spod:eeeeeeeeeeeeeeee]] [[llm-mask:k8snode:ffffffffffffffff]] phase=Pending restarts=0,
```

The agent can refer to the sentinels, but it should not try to decode or rewrite
them.

## 4. Run a read-only follow-up through `llm-mask exec`

After a project explicitly enables trusted read-only exec, pass the selected
namespace and pod sentinels verbatim:

```sh
llm-mask exec -- kubectl describe -n '[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]]' '[[llm-mask:k8spod:bbbbbbbbbbbbbbbb]]'
```

When a `namespace/<name>` or `ns/<name>` sentinel is used as a `kubectl -n` or
`--namespace` value, `llm-mask exec` resolves it locally and passes only the bare
namespace name to `kubectl`. The pod sentinel remains a full `pod/<name>`
resource reference.

Quote ASCII sentinels in shell commands. In zsh, an unquoted token like
`[[llm-mask:k8spod:2222222222222222]]` is treated as a glob pattern and can
fail before `llm-mask` receives it:

```text
zsh: no matches found: [[llm-mask:k8spod:2222222222222222]]
```

Use single quotes, or prefix the command with `noglob`:

```sh
noglob llm-mask exec -- kubectl describe -n [[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]] [[llm-mask:k8spod:bbbbbbbbbbbbbbbb]]
```

`llm-mask exec` resolves the sentinel locally for `kubectl`, then masks
stdout/stderr before returning output to the agent.

A namespace-qualified `kubectl describe` follow-up was smoke-tested against a
real cluster with output suppressed and exited successfully.

Safe metadata-only permission checks can also be routed through `exec`:

```sh
llm-mask exec -- kubectl auth can-i get pods --all-namespaces
```

Example output:

```text
yes
```

Do not run `kubectl get secret`, `kubectl logs -f`, mutation commands, shell
pipelines, or redirects through `exec`; the read-only policy rejects sensitive or
bypass-shaped commands by design.
