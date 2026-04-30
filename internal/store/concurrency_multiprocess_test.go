package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSQLiteConcurrentSubprocessWriters(t *testing.T) {
	if os.Getenv("OPSMASK_STORE_CHILD") == "1" {
		childInsert(t, os.Getenv("OPSMASK_STORE_PATH"))
		return
	}
	path := filepath.Join(t.TempDir(), "mapping.sqlite")
	const workers = 8
	cmds := make([]*exec.Cmd, 0, workers)
	// Pre-size the output buffers so their addresses remain stable while
	// the exec goroutines are writing into them.
	outputs := make([]bytes.Buffer, workers)
	for i := 0; i < workers; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestSQLiteConcurrentSubprocessWriters")
		cmd.Env = append(os.Environ(), "OPSMASK_STORE_CHILD=1", "OPSMASK_STORE_PATH="+path)
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
		cmds = append(cmds, cmd)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("child %d: %v: %s", i, err, outputs[i].String())
		}
	}
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.List(context.Background(), "ip4", 1100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1000 {
		t.Fatalf("rows=%d want 1000", len(rows))
	}
}

func childInsert(t *testing.T, path string) {
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < 1000; i++ {
		idx := fmt.Sprintf("%016x", i)
		m := Mapping{Type: "ip4", Index: idx, HMACFull: []byte(fmt.Sprintf("%032d", i)), RealValue: []byte(fmt.Sprintf("10.0.0.%d", i)), FirstSeenAt: time.Now()}
		if err := st.Insert(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.sqlite")
	const workers = 8
	const rows = 200
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			st, err := OpenSQLite(path)
			if err != nil {
				errs <- err
				return
			}
			defer st.Close()
			for i := 0; i < rows; i++ {
				idx := fmt.Sprintf("%016x", i)
				m := Mapping{Type: "ip4", Index: idx, HMACFull: []byte(fmt.Sprintf("%032d", i)), RealValue: []byte(fmt.Sprintf("10.0.0.%d", i)), FirstSeenAt: time.Now()}
				if err := st.Insert(context.Background(), m); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	list, err := st.List(context.Background(), "ip4", rows+10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != rows {
		t.Fatalf("rows=%d want %d", len(list), rows)
	}
}
