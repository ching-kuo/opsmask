package engine

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ching-kuo/llm-mask/internal/detect"
	"github.com/ching-kuo/llm-mask/internal/pseudo"
	"github.com/ching-kuo/llm-mask/internal/store"
)

func BenchmarkMixedSecretsCorpus(b *testing.B) {
	normal := strings.Repeat("INFO request completed status=200 latency_ms=17 component=api\n", 999)
	rare := "2026-04-24T00:00:00Z WARN user alice@example.com ip 10.0.0.1 token AKIAIOSFODNN7EXAMPLE\n"
	corpus := []byte(strings.Repeat(normal+rare, 768))
	rules, err := detect.BuiltinRules()
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(corpus)))
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st, err := store.OpenSQLite(filepath.Join(b.TempDir(), "mapping.sqlite"))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		_, err = Process(context.Background(), bytes.NewReader(corpus), io.Discard, rules, pseudo.New([]byte("01234567890123456789012345678901"), st), Options{ASCIITokens: true})
		b.StopTimer()
		_ = st.Close()
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
