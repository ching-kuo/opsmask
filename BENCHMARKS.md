# Benchmarks

Local benchmark evidence from the development machine:

- Hardware: Apple M1 Max, macOS arm64.
- Command: `go test ./internal/engine -bench=BenchmarkMixedSecretsCorpus -benchmem -run '^$' -benchtime=1x`.
- Corpus: synthetic mixed log corpus (~45 MiB) with sparse email/IP/AWS-key lines among normal log lines.
- Result: `1503710959 ns/op`, `31.68 MB/s`, `630476632 B/op`, `853229 allocs/op`.

The current implementation meets the v1 30 MB/s benchmark gate on this named
local corpus. Allocation volume remains a follow-up optimization target;
specialized scanners for IPv4, email, UUID, MAC, and IPv6 should reduce heap
pressure in a later pass.

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
