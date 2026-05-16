# Claude Code Bash Hook — Integration Test Report

**Date:** 2026-05-05 (initial); retested 2026-05-05 (k8s fixes); retested 2026-05-05 (OpenStack/Kolla); retested 2026-05-05 (PSL hostname fix); retested 2026-05-06 (extended coverage); retested 2026-05-06 (k8s YAML/path separator fix)
**Branch:** feat/claude-code-bash-hook  
**Environments tested:**
- Kubernetes cluster (5-node: 3 control-plane, 2 worker)
- OpenStack/Kolla node at 192.0.2.1 (`/var/log/kolla/`)

## Setup

Hook installed via `opsmask install claude-code`, writing a PreToolUse Bash hook to
`.claude/settings.local.json` pointing to the per-project shim. Uninstalled after
each test round via `opsmask uninstall claude-code`.

## Commands Tested

### Kubernetes

| Command | Purpose |
|---|---|
| `kubectl get nodes -o wide` | Node IPs, roles, and AGE column |
| `kubectl get pods -A -o wide` | Pod IPs, NOMINATED NODE header |
| `kubectl get svc -A` | ClusterIPs and ExternalName hostnames |
| `kubectl get ingress -A` | Ingress host names, LB address, compound resource names |
| `kubectl describe node <name>` | Node labels, annotations, addresses |
| `kubectl logs ingress-nginx` (15 lines) | nginx access logs with client IPs, hex request IDs |
| `kubectl logs authentik-server` (15 lines) | Structured JSON logs with IPs and request IDs |
| `kubectl get configmap nonexistent` | Error message context preservation |
| `kubectl get ingress nonexistent` | Error message context preservation |
| `kubectl get secret opsmask-test -o yaml` | Secret YAML with base64 data fields |
| `kubectl describe pod opsmask-env-test` | Pod env vars including `cluster.local` hostname and literal secrets |

### OpenStack / Kolla (via SSH to 192.0.2.1)

| Command | Purpose |
|---|---|
| `ls /var/log/kolla/nova/` | Log directory with rotated filenames |
| `ls /var/log/kolla/haproxy/` | HAProxy log directory with date-suffixed filenames |
| `tail nova-api.log` | Nova API: UUIDs, hex IDs, client IPs, Python logger names |
| `tail nova-api-access.log` | Apache combined log format: IPs, hex tenant IDs, UUIDs in paths |
| `tail neutron-server.log` | Neutron: UUIDs, hex IDs, Python module paths |
| `tail keystone.log` | Keystone: UUIDs, Python module paths |
| `tail nova-compute.log` | Nova compute: UUIDs, oslo.service logger names |
| `tail haproxy_latest.log` | HAProxy JSON log: IPs, port numbers, backend names |
| `ip addr show` | Network interfaces: IPs, MACs, IPv6, CIDR, OVN tap interfaces |

### Other

| Command | Purpose |
|---|---|
| `git log --oneline` | 7-char short commit SHAs — confirm below hex_id threshold |

## Results

### Correctly Masked

| Type | Token | Example location |
|---|---|---|
| IPv4 addresses | `[[opsmask:ip4:...]]` | Pod IPs, ClusterIPs, LB IP, client IPs in nginx/Nova API/HAProxy logs, `ip addr` |
| IPv6 addresses | `[[opsmask:ip6:...]]` | `ip addr` interface link-local addresses |
| MAC addresses | `[[opsmask:mac:...]]` | `ip addr` link/ether lines, broadcast addresses |
| Hostnames (FQDNs) | `[[opsmask:hostname:...]]` | Ingress HOSTS column, ExternalName service, `domain_url` JSON field, `cluster.local` DNS names |
| Hex / request IDs | `[[opsmask:hex_id:...]]` | nginx request IDs, authentik `request_id`, Nova user/project IDs, OVN hash ring IDs, hex-suffix HAProxy log files, SHA256 image digests |
| UUIDs | `[[opsmask:uuid:...]]` | k8s node annotations, OpenStack `req-<uuid>`, Nova resource IDs, Keystone request IDs, OVN metadata namespace IDs in `ip addr` |
| Email addresses | `[[opsmask:email:...]]` | Netcraft user-agent contact address |
| Kubernetes resource names | `[[opsmask:k8s*:...]]` | Secret, pod, namespace names in kubectl output |

JSON structured log masking (authentik, ingress-nginx JSON) correctly reaches into field
values without breaking log structure. OpenStack oslo.log format (plaintext, dict-in-string)
is also handled correctly for credential and ID fields. Apache combined log format correctly
masks client IPs and hex tenant IDs in request paths.

`cluster.local` internal DNS names are correctly masked as hostnames — `local` is in the
`defaultInternalTLDs` allowlist in `HostnameCheckFor`.

### False Positives

#### Accepted (low severity)

| Pattern | Category | Example | Severity |
|---|---|---|---|
| Chrome version string `136.0.x.x` in user-agent | ip4 | `Chrome/[[opsmask:ip4:...]] Safari` | Low — version numbers structurally match IPs; accepted tradeoff |
| Non-`kubernetes.io` label domain prefixes | hostname | Custom cluster-provider FQDN label keys in `kubectl describe node` | Low — these are real FQDNs; `kubernetes.io/*` correctly preserved |
| `settings.local.json` in uninstall output | hostname | `uninstalled ... [[opsmask:hostname:...]]` | Low — cosmetic, file path only |
| `ingresses.networking.k8s.io` in error text | hostname | `[[opsmask:hostname:...]] "nonexistent" not found` | Low — `k8s.io` is a real ICANN TLD; API group names structurally indistinguishable |
| Toleration keys `node.kubernetes.io/not-ready` | hostname | `[[opsmask:hostname:...]]/not-ready:NoExecute` | Low — `kubernetes.io` is a real domain; same class as label key masking |

#### Fixed in previous revisions

| Pattern | Category | Previous rendering | After fix |
|---|---|---|---|
| Worker node AGE column | k8snode | `node   [[opsmask:k8snode:...]]` (`10h` masked) | `node   19h` — AGE preserved |
| Ingress compound names | k8singress | `ladder-[[opsmask:k8singress:...]]` | `ladder-ingress` — full name preserved |
| Configmap name in error text | k8sconfigmap | full span tokenized | `configmaps "[[opsmask:k8sconfigmap:...]]" not found` |
| Python module logger names | hostname | `nova.api.openstack.wsgi` → `[[opsmask:hostname:...]]` | Module path preserved (PSL fix) |
| Dot-separated log filenames | hostname | `nova-api.log` partially tokenized | All filenames clean (PSL fix) |
| `metadata:` key in `kubectl get secret -o yaml` | k8ssecret | `kind: Secret\n[[opsmask:k8ssecret:...]]:` | `kind: Secret\nmetadata:` — separator restricted to `[ \t]+` (no `\n`) |
| `secrets/kubernetes.io` path in pod describe | k8ssecret | `/var/run/secrets/[[opsmask:k8ssecret:]].io/serviceaccount` | `kubernetes.io` preserved — trailing `.` rejects the capture |

### Coverage Gaps (not false positives — design decisions)

| Data type | Command | Observation |
|---|---|---|
| Base64-encoded secret values | `kubectl get secret -o yaml` | `data.api-key: c2stdGVzdC1...` shown as plaintext base64 — not decoded/matched |
| Plain-text env var values in pod spec | `kubectl describe pod` | `API_KEY: sk-test-supersecretapikey123456789` and `DB_PASSWORD: hunter2-this-is-fake` visible |

Both are by design: opsmask has no generic "secret value after a key" detector. Addressing
the env var gap would require a heuristic that scans for lines matching `KEY: value` where
the key name suggests a credential — high false-positive risk for benign values like
`LOG_LEVEL: debug`.

### Not Masked (Expected)

- Node names (`control-plane-nova-1-xqyseq`, `nodes-nova-m2v1oq`) — correct
- `kubernetes.io/*` label keys — correctly allowlisted
- Log metadata fields (`logger`, `level`, `method`, `status`, `pid`) — correct
- Timestamps — correct
- Ingress resource names in NAME column (`ladder-ingress`, `open-webui-ingress`) — correct
- Worker node AGE values (`19h`) — correct
- Python module paths, all services — not masked (PSL fix confirmed)
- Log filenames and HAProxy date-suffix directory entries — not masked (PSL fix confirmed)
- Git short SHAs (`ccc678c`, `21aa0d0`) — not masked; 7 chars is below hex_id 32-char minimum
- `ip addr` interface names (`eno1`, `bond0`, `bond0.1115`, `tap*`), MTU, qdisc, state — not masked
- CIDR prefix lengths (`/8`, `/24`, `/64`, `/128`) — not masked
- Port numbers in HAProxy logs (`:31984`, `:9101`) — not masked
- Backend names in HAProxy (`status/HTTP`, `metrics/HTTP`) — not masked
- `openstack01` bare hostname (no dots) — not masked; correct, single-label names excluded by regex
- HAProxy JSON `log_level`, `programname` fields — not masked

## Conclusions

### PSL hostname fix: no regressions, `cluster.local` correctly handled

All PSL-fix benefits confirmed. `cluster.local` names mask correctly via the
`defaultInternalTLDs` allowlist. HAProxy directory listings and all OpenStack Python logger
names are clean.

### `ip addr show`: comprehensive network interface masking

All network identifiers masked cleanly (IPv4, IPv6, MAC, OVN UUIDs). Interface names,
CIDR prefixes, port numbers, and non-identifier metadata all preserved. This is a strong
result for network diagnostics workflows.

### Plain-text env var credentials: by-design coverage gap

`kubectl describe pod` output reveals plain-text credentials injected as env vars (not
from Secrets). This is not a regression — opsmask has no heuristic for generic key=value
credential detection — but operators should be aware that env-var–injected secrets are
not protected.

### Git log: short SHAs correctly excluded

7-char abbreviated commits do not trigger hex_id (32-char minimum floor). Full 40-char
SHAs would be masked; abbreviated refs are not.
