package exec

import (
	"context"
	"testing"
)

func TestResolveSentinels(t *testing.T) {
	values := map[string][]byte{"k8spod:0123456789abcdef": []byte("api-7d4f-xyz")}
	got, err := Resolve(context.Background(), []string{"kubectl", "describe", "pod", "⟪llm-mask:k8spod:0123456789abcdef⟫"}, func(typ, idx string) ([]byte, bool, error) {
		v, ok := values[typ+":"+idx]
		return v, ok, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[3] != "api-7d4f-xyz" {
		t.Fatalf("resolved arg = %q", got[3])
	}
}

func TestResolveIsOnePassAndEscapedFormsAreInert(t *testing.T) {
	values := map[string][]byte{"host:0123456789abcdef": []byte("literal-⟪llm-mask:host:ffffffffffffffff⟫")}
	got, err := Resolve(context.Background(), []string{"echo", "[LLM_MASK_ESCAPED_SENTINEL:x]", "[[llm-mask:host:0123456789abcdef]]"}, func(typ, idx string) ([]byte, bool, error) {
		v, ok := values[typ+":"+idx]
		return v, ok, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != "[LLM_MASK_ESCAPED_SENTINEL:x]" {
		t.Fatalf("escaped sentinel changed: %q", got[1])
	}
	if got[2] != "literal-⟪llm-mask:host:ffffffffffffffff⟫" {
		t.Fatalf("replacement was rescanned: %q", got[2])
	}
}

func TestResolveASCIIAndUnicodeForms(t *testing.T) {
	values := map[string][]byte{"k8spod:0123456789abcdef": []byte("api-7d4f-xyz")}
	lookup := func(typ, idx string) ([]byte, bool, error) {
		v, ok := values[typ+":"+idx]
		return v, ok, nil
	}
	for _, form := range []string{"⟪llm-mask:k8spod:0123456789abcdef⟫", "[[llm-mask:k8spod:0123456789abcdef]]"} {
		got, err := Resolve(context.Background(), []string{"kubectl", "describe", "pod", form}, lookup)
		if err != nil {
			t.Fatalf("form %q: %v", form, err)
		}
		if got[3] != "api-7d4f-xyz" {
			t.Fatalf("form %q resolved to %q", form, got[3])
		}
	}
}

func TestResolveNormalizesKubectlNamespaceFlagResourceReferences(t *testing.T) {
	values := map[string][]byte{
		"k8snamespace:aaaaaaaaaaaaaaaa": []byte("namespace/demo-safe"),
		"k8spod:bbbbbbbbbbbbbbbb":       []byte("pod/api-7d4f-xyz"),
	}
	lookup := func(typ, idx string) ([]byte, bool, error) {
		v, ok := values[typ+":"+idx]
		return v, ok, nil
	}
	got, err := Resolve(context.Background(), []string{
		"kubectl", "describe", "-n", "[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]]", "[[llm-mask:k8spod:bbbbbbbbbbbbbbbb]]",
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got[3] != "demo-safe" {
		t.Fatalf("namespace flag resolved to %q, want bare namespace", got[3])
	}
	if got[4] != "pod/api-7d4f-xyz" {
		t.Fatalf("pod arg resolved to %q", got[4])
	}
}

func TestResolveNormalizesKubectlNamespaceEqualsResourceReferences(t *testing.T) {
	values := map[string][]byte{
		"k8snamespace:aaaaaaaaaaaaaaaa": []byte("ns/demo-safe"),
		"k8spod:bbbbbbbbbbbbbbbb":       []byte("pod/api-7d4f-xyz"),
	}
	lookup := func(typ, idx string) ([]byte, bool, error) {
		v, ok := values[typ+":"+idx]
		return v, ok, nil
	}
	got, err := Resolve(context.Background(), []string{
		"kubectl", "describe", "--namespace=[[llm-mask:k8snamespace:aaaaaaaaaaaaaaaa]]", "[[llm-mask:k8spod:bbbbbbbbbbbbbbbb]]",
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got[2] != "--namespace=demo-safe" {
		t.Fatalf("namespace flag resolved to %q, want --namespace=demo-safe", got[2])
	}
}

func TestResolveFailsClosed(t *testing.T) {
	_, err := Resolve(context.Background(), []string{"echo", "⟪llm-mask:k8spod:ffffffffffffffff⟫"}, func(typ, idx string) ([]byte, bool, error) {
		return nil, false, nil
	})
	if err == nil {
		t.Fatal("expected unknown sentinel error")
	}
}
