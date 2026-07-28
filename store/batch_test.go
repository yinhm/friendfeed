package store

import (
	"errors"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

func TestApplyBatchCommitsAtomically(t *testing.T) {
	db := NewStore(t.TempDir())
	defer db.Close()

	if err := db.Put([]byte("old"), []byte("value")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete([]byte("old"), nil); err != nil {
			return err
		}
		return batch.Set([]byte("new"), []byte("value"), nil)
	}); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	if got, err := db.Get([]byte("old")); err != nil || got != nil {
		t.Fatalf("old = %q, %v; want missing", got, err)
	}
	if got, err := db.Get([]byte("new")); err != nil || string(got) != "value" {
		t.Fatalf("new = %q, %v; want value", got, err)
	}
}

func TestApplyBatchCallbackErrorDoesNotCommit(t *testing.T) {
	db := NewStore(t.TempDir())
	defer db.Close()

	if err := db.Put([]byte("old"), []byte("value")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	wantErr := errors.New("stop")
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete([]byte("old"), nil); err != nil {
			return err
		}
		if err := batch.Set([]byte("new"), []byte("value"), nil); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyBatch error = %v; want %v", err, wantErr)
	}

	if got, err := db.Get([]byte("old")); err != nil || string(got) != "value" {
		t.Fatalf("old = %q, %v; want original value", got, err)
	}
	if got, err := db.Get([]byte("new")); err != nil || got != nil {
		t.Fatalf("new = %q, %v; want missing", got, err)
	}
}

func TestApplyBatchEmptyCallbackIsNoOp(t *testing.T) {
	db := NewStore(t.TempDir())
	defer db.Close()

	if err := db.Put([]byte("existing"), []byte("value")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ApplyBatch(func(*pebble.Batch) error {
		return nil
	}); err != nil {
		t.Fatalf("empty ApplyBatch: %v", err)
	}

	if got, err := db.Get([]byte("existing")); err != nil || string(got) != "value" {
		t.Fatalf("existing = %q, %v; want unchanged value", got, err)
	}
}
