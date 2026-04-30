# Benchmarks

Local benchmark evidence from the development machine:

- Hardware: Apple M1 Max, macOS arm64.
- Command: `go test ./internal/engine -bench=BenchmarkMixedSecretsCorpus -benchmem -run '^$' -benchtime=3x`.
- Corpus: synthetic mixed log corpus (~45 MiB) with sparse email/IP/AWS-key
  lines among normal log lines.
- Result (current Unreleased branch): `2368788444 ns/op`, `20.11 MB/s`,
  `466288285 B/op`, `858344 allocs/op`.

The Unreleased "engine hot path" optimizations (precomputed `[][]byte`
keywords, `bytes.Index` keyword scans, `InertEscape` sentinel fast-path)
delivered roughly **+42% throughput and -17% memory** on this corpus
relative to the prior `main` baseline (`13.86 MB/s`, `638 MB B/op`).

Allocation volume remains a follow-up optimization target; specialized
scanners for IPv4, email, UUID, MAC, and IPv6 — and a buffer pool for the
per-chunk `bytes.Buffer` in `engine.maskChunk` — should reduce heap
pressure further in a later pass.

**Note.** The original v1 benchmark line in this file claimed `31.68 MB/s`
on a `~45 MiB` corpus; that number reflected the earlier rule set. Engine
throughput has since dropped naturally as the gitleaks-derived secret
ruleset and Kubernetes-resource detectors expanded scan surface. The
current numbers above are measured on the present rule set.

Concurrency evidence:

- `TestSQLiteConcurrentSubprocessWriters` starts 8 subprocesses against the
  same SQLite mapping and inserts 1000 overlapping values each without
  corruption or duplicate rows.
- `TestSQLiteConcurrentWriters` covers 8 concurrent in-process stores as a fast
  regression companion.

Round-trip evidence:

- CLI smoke in Ralph verification covers `init`, `mask --summary`, and PTY
  `unmask` restoring email/IP values from the generated SQLite mapping while
  keeping destroyed AWS keys unrecoverable.
