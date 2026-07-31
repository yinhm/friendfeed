package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preserveIndexer saves the global Indexer and restores it on cleanup so
// lifecycle tests do not leak global state into each other.
func preserveIndexer(t *testing.T) {
	t.Helper()
	saved := Indexer
	t.Cleanup(func() { Indexer = saved })
}

// A corrupted index (garbage index_meta.json) must surface as an error from
// the error-returning constructors, and must not poison the global Indexer.
func TestOpenIndexCorrupted(t *testing.T) {
	preserveIndexer(t)

	indexPath := filepath.Join(t.TempDir(), "index")
	require.NoError(t, os.MkdirAll(indexPath, 0o755))
	// bleve persists its metadata in index_meta.json; a short garbage file
	// fails JSON decoding and is reported as ErrorIndexMetaCorrupt rather
	// than ErrorIndexMetaMissing, exercising the old log.Fatal path.
	require.NoError(t, os.WriteFile(filepath.Join(indexPath, "index_meta.json"), []byte("x"), 0o644))

	idx, err := OpenIndex(indexPath)
	assert.Error(t, err)
	assert.ErrorIs(t, err, bleve.ErrorIndexMetaCorrupt)
	assert.Nil(t, idx)

	Indexer = nil
	err = InitIndexServiceE(indexPath)
	assert.Error(t, err)
	assert.Nil(t, Indexer, "failed init must not assign the global Indexer")

	// The compatibility entry point keeps its panic-on-error contract.
	assert.Panics(t, func() { NewIndex(indexPath) })
	assert.Panics(t, func() { InitIndexService(indexPath) })
	assert.Nil(t, Indexer)
}

// gse loads its default dictionary from data files shipped inside the gse
// module itself (resolved relative to the gse package source), so a missing
// default dictionary cannot be simulated honestly; instead an explicit
// nonexistent dictionary path is passed to trigger the LoadDict error.
func TestNewTokenizerEMissingDict(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dict.txt")

	tokenizer, err := NewTokenizerE(missing)
	assert.Error(t, err)
	assert.Nil(t, tokenizer)

	// The bleve registry constructor must propagate the error instead of
	// panicking, and still succeeds with the default dictionary.
	_, err = NewConstructor(map[string]any{}, nil)
	assert.NoError(t, err)

	// The default dictionary is bundled with the gse module, so the
	// compatibility entry point cannot be made to fail here; its success
	// path is covered by TestTokenizerTerms.
}

// CloseIndexService is nil-safe and idempotent: bleve Close runs at most
// once per initialized index, and repeated calls return safely.
func TestCloseIndexServiceIdempotent(t *testing.T) {
	preserveIndexer(t)

	Indexer = nil
	assert.NoError(t, CloseIndexService(), "closing a nil Indexer must be a no-op")

	indexPath := filepath.Join(t.TempDir(), "index")
	require.NoError(t, InitIndexServiceE(indexPath))
	require.NotNil(t, Indexer)

	assert.NoError(t, CloseIndexService())
	assert.Nil(t, Indexer, "close must clear the global Indexer")
	assert.NoError(t, CloseIndexService(), "repeated close must not call bleve Close again")
}
