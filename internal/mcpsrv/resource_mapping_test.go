package mcpsrv_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/mcpsrv"
	mcpruntime "github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/ching-kuo/opsmask/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func startServerWithStore(t *testing.T) (*mcp.ClientSession, *mcpruntime.Env, func()) {
	t.Helper()
	rt := newTestRuntime(t)
	srv := mcpsrv.NewServer(rt, nil)
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() { _ = srv.Run(ctx, serverT) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect: %v", err)
	}
	return sess, rt, func() { _ = sess.Close(); cancel() }
}

func seedMappings(t *testing.T, st store.Store, typ string, n int, plaintextPrefix string) []store.Mapping {
	t.Helper()
	rows := make([]store.Mapping, 0, n)
	for i := 0; i < n; i++ {
		hmac := bytes.Repeat([]byte{byte(i + 1)}, 32)
		idx := hex.EncodeToString(hmac[:8])
		rows = append(rows, store.Mapping{
			Type:        typ,
			Index:       idx,
			HMACFull:    hmac,
			RealValue:   []byte(fmt.Sprintf("%s-%d", plaintextPrefix, i)),
			FirstSeenAt: time.Now(),
		})
	}
	if err := st.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return rows
}

func readMapping(t *testing.T, sess *mcp.ClientSession, uri string) []byte {
	t.Helper()
	res, err := sess.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource %s: %v", uri, err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("no contents")
	}
	return []byte(res.Contents[0].Text)
}

func TestMappingResourceLimitClamp(t *testing.T) {
	sess, rt, cleanup := startServerWithStore(t)
	defer cleanup()
	seedMappings(t, rt.Store, "ip4", 10, "1.2.3")

	body := readMapping(t, sess, "opsmask://mapping/ip4?limit=5")
	var got mcpsrv.MappingResourceBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(got.Entries))
	}
	if !got.Truncated {
		t.Fatal("truncated = false, want true")
	}

	body = readMapping(t, sess, "opsmask://mapping/ip4?limit=20")
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Entries) != 10 {
		t.Fatalf("entries = %d, want 10", len(got.Entries))
	}
	if got.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestMappingResourceMaxLimitClamp(t *testing.T) {
	sess, rt, cleanup := startServerWithStore(t)
	defer cleanup()
	seedMappings(t, rt.Store, "ip4", 600, "10.")

	body := readMapping(t, sess, "opsmask://mapping/ip4?limit=1000")
	var got mcpsrv.MappingResourceBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Entries) > 500 {
		t.Fatalf("entries = %d, want <= 500", len(got.Entries))
	}
}

func TestMappingResourceUnknownType(t *testing.T) {
	sess, _, cleanup := startServerWithStore(t)
	defer cleanup()

	body := readMapping(t, sess, "opsmask://mapping/nonexistent_type")
	var got mcpsrv.MappingResourceBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(got.Entries))
	}
	if got.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

// TestMappingResourceNoPlaintext verifies that the resource body never carries
// plaintext bytes for any seeded value, regardless of how the client probes.
func TestMappingResourceNoPlaintext(t *testing.T) {
	sess, rt, cleanup := startServerWithStore(t)
	defer cleanup()
	markers := []string{"ZZZ-PLAIN-MARKER-1", "ZZZ-PLAIN-MARKER-2", "ZZZ-PLAIN-MARKER-3"}
	rows := make([]store.Mapping, 0, len(markers))
	for i, m := range markers {
		hmac := bytes.Repeat([]byte{byte(i + 100)}, 32)
		idx := hex.EncodeToString(hmac[:8])
		rows = append(rows, store.Mapping{
			Type: "marker", Index: idx, HMACFull: hmac,
			RealValue: []byte(m), FirstSeenAt: time.Now(),
		})
	}
	if err := rt.Store.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := readMapping(t, sess, "opsmask://mapping/marker")
	for _, m := range markers {
		if bytes.Contains(body, []byte(m)) {
			t.Fatalf("resource body leaked plaintext marker %q: %s", m, string(body))
		}
	}
}

// TestMappingResourceNoHMAC verifies the resource body does not carry HMAC
// bytes in any common encoding. An attacker who controls one mapping store
// could otherwise correlate mappings across stores via HMAC equality.
func TestMappingResourceNoHMAC(t *testing.T) {
	sess, rt, cleanup := startServerWithStore(t)
	defer cleanup()
	rows := seedMappings(t, rt.Store, "ip4", 3, "1.2.3")

	body := readMapping(t, sess, "opsmask://mapping/ip4")
	for _, r := range rows {
		encodings := []string{
			string(r.HMACFull),
			hex.EncodeToString(r.HMACFull),
			strings.ToUpper(hex.EncodeToString(r.HMACFull)),
			base64.StdEncoding.EncodeToString(r.HMACFull),
			base64.RawStdEncoding.EncodeToString(r.HMACFull),
			base64.URLEncoding.EncodeToString(r.HMACFull),
			base64.RawURLEncoding.EncodeToString(r.HMACFull),
		}
		// The token is the first 8 bytes hex-encoded — that IS exposed.
		// Filter encodings that match the token form to avoid a false positive.
		idxHex := hex.EncodeToString(r.HMACFull[:8])
		for _, enc := range encodings {
			if enc == idxHex {
				continue
			}
			// Skip encodings that are too short to be meaningful (the token
			// hex is 16 chars; HMAC encodings are 32+).
			if len(enc) < 16 {
				continue
			}
			if bytes.Contains(body, []byte(enc)) {
				t.Fatalf("resource leaked HMAC encoding %q in body: %s", enc, string(body))
			}
		}
	}
}

func TestMappingResourceCapabilitiesSubscribeFalse(t *testing.T) {
	sess, _, cleanup := startServerWithStore(t)
	defer cleanup()
	res := sess.InitializeResult()
	if res.Capabilities.Resources == nil {
		t.Fatal("Resources capability not advertised")
	}
	if res.Capabilities.Resources.Subscribe {
		t.Fatal("Subscribe must be false")
	}
}

func TestMappingResourceURIValidation(t *testing.T) {
	sess, _, cleanup := startServerWithStore(t)
	defer cleanup()
	cases := []string{
		"opsmask://mapping",                  // no type
		"opsmask://wrong/ip4",                // wrong host
		"opsmask://mapping/ip4?limit=0",      // bad limit
		"opsmask://mapping/ip4?limit=abc",    // bad limit
	}
	for _, uri := range cases {
		_, err := sess.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Fatalf("expected error for %s", uri)
		}
	}
}
