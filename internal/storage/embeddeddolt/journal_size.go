//go:build cgo

package embeddeddolt

import (
	"errors"
	"os"
	"path/filepath"
)

// chunkJournalFile is the name Dolt gives its append-only chunk journal. Dolt
// does not export it. TestEmbeddedAutoGCRunsAfterAWrite fails if a Dolt upgrade
// renames the file, which would otherwise silently stop automatic garbage
// collection.
const chunkJournalFile = "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"

// ChunkJournalSize returns the size in bytes of the active database's chunk
// journal, or 0 when it has none. It only stats a file, so it is cheap enough
// to call after every write.
func (s *EmbeddedDoltStore) ChunkJournalSize() (int64, error) {
	if s.closed.Load() {
		return 0, errClosed
	}
	info, err := os.Stat(filepath.Join(s.dataDir, s.database, ".dolt", "noms", chunkJournalFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
