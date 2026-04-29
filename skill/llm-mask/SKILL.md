---
name: llm-mask
description: llm-mask helps Claude Code analyze logs while reducing exposure of secrets and identifiers. Use it when handling prod logs, kubectl logs, journalctl output, ssh output, requests to mask/redact/sanitize logs, or when investigating a masked entity requires a follow-up read-only command such as kubectl describe, dig, or nslookup.
---

# llm-mask

Route log-fetching commands through `llm-mask mask` before analyzing the output.

Examples:

```sh
kubectl logs deploy/api | llm-mask mask
journalctl -u app.service | llm-mask mask
ssh host 'tail -1000 /var/log/app.log' | llm-mask mask
```

## Kubernetes metadata

For Kubernetes metadata, prefer read-only commands that print one concise record
per line and keep resource nouns next to resource names. This makes masking more
reliable than raw table output or full YAML dumps.

If the task involves `kubectl get`, pod metadata, namespace-qualified follow-up
commands, zsh quoting, or converting YAML/JSON fields into mask-friendly lines,
read `references/kubernetes.md`.

If the user already pasted raw `kubectl get pods` table output, reshape it into
one `namespace/<ns> pod/<pod> node/<node>` record per line *before* running
`llm-mask mask`. The detectors key off those resource-noun prefixes; bare table
columns are unreliable to mask.

Agents must preserve sentinel tokens verbatim:

- `⟪llm-mask:<type>:<index>⟫`
- `[[llm-mask:<type>:<index>]]`
- `[LLM_MASK_ESCAPED_SENTINEL:...]`

Do not paraphrase, wrap, lower-case, split, or "clean up" those token forms in
reports. They are the bridge back to the user's local mapping store.

## If something secret-looking survives masking

If the output of `llm-mask mask` still contains something that obviously looks
like a secret (a JWT, a `sk_live_...` / `sk_test_...` Stripe-style key, a
provider token, a private key block), treat it as a masker bug, not as your
problem to fix in the report:

1. Do not paste the raw value into your reply or any saved file.
2. Tell the user the masker missed it so they can extend the detectors (via
   `docs/CUSTOM_DETECTORS.md` for project-local regex, or by reporting the
   gap upstream) before relying on the pipeline.
3. Stop the analysis on that value; do not try to reason about it further.

The skill does not ask you to do a second-pass redaction. The masker is the
boundary; if a secret crossed it, the right move is to surface the gap, not
to paper over it in the agent layer.

Never invoke `llm-mask unmask` from an agent session. After writing the final
report, tell the user to run `llm-mask unmask < report.md` in their own
terminal.

This skill is advisory, not enforcement. The safety property comes from keeping
the mapping store local and out of LLM reach.

## Follow-up commands with `llm-mask exec`

When investigation needs the real value behind a sentinel, **run `llm-mask exec`
yourself** — do not stop at writing a command list for the user. The wrapper
resolves sentinels locally and re-masks stdout/stderr before the output reaches
you, so you never see raw values and the masking pipeline stays intact.

Default to running. Hand a command list back to the user only when:

- They explicitly said they will run it themselves (or said "just write the
  commands").
- `exec` rejected the command (`deny_layer_*` / `not_in_allow_list`) and the
  current scope tier cannot satisfy the workflow.
- There is no `.llm-mask` mapping in the working directory, or the binary is
  not available in this environment.

Form:

```sh
llm-mask exec -- <cmd> <args containing sentinels>
```

Examples:

```sh
llm-mask exec -- kubectl describe pod '⟪llm-mask:k8spod:0123456789abcdef⟫'
llm-mask exec -- nslookup '[[llm-mask:hostname:0123456789abcdef]]'
llm-mask exec -- dig '⟪llm-mask:hostname:0123456789abcdef⟫'
```

The wrapper resolves sentinels locally and re-masks stdout/stderr before the
output returns to the agent. You never need to see the real values.

Sentinel-passing rules:

- Pass sentinels verbatim.
- Do not paraphrase, lowercase, ASCII-translate, split, quote-strip, or "fix"
  them.
- Both `⟪llm-mask:type:index⟫` and `[[llm-mask:type:index]]` are accepted.
- `[LLM_MASK_ESCAPED_SENTINEL:...]` is inert source text; pass it through
  unchanged and do not try to decode it.

Do not use shell redirects (`>`, `>>`, `tee`, `&>`), background writes, or
shell pipelines inside `exec`; they can bypass the masking pipeline. Shells
(`bash`, `sh`, `zsh`, etc.) are rejected by the hard deny-list. Run separate
`exec` calls instead.

`exec` runs under a project-chosen scope tier:

- `read-only` (default): kubectl read verbs, DNS tools, stdin-only `jq`.
- `investigate`: broader read-only SRE commands plus arbitrary file readers.
- `freeform`: any non-denied command.

Changing tiers is the user's decision, not yours. If `exec` rejects a command
with `deny_layer_*` or `not_in_allow_list`, do not retry with bypass-shaped
variants and do not ask for shell access. Report that the current project tier
does not permit the command and stop.

On shared bastion/jump-host environments, resolved argv may be briefly visible
to other local users through process listings unless the host hides other users'
processes. Do not recommend enabling `exec` there unless the user accepts that
risk.
