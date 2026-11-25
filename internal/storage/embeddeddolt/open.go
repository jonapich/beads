//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	doltembed "github.com/dolthub/driver/v2"
)

// validIdentifier matches safe SQL identifiers (letters, digits, underscores).
// Hyphens are excluded because database names are interpolated into system
// variable identifiers (@@<db>_head_ref) where hyphens are invalid.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const (
	commitName  = "beads"
	commitEmail = "beads@local"

	// lockWaitInitialInterval and lockWaitMaxInterval bound the exponential
	// backoff an engine open uses while another process holds the lock.
	lockWaitInitialInterval = 50 * time.Millisecond
	lockWaitMaxInterval     = time.Second
)

// OpenSQL opens an embedded Dolt database at dir. The returned cleanup
// function closes both the *sql.DB and the underlying connector.
func OpenSQL(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
	dsn := buildDSN(dir, database)

	cfg, err := doltembed.ParseDSN(dsn)
	if err != nil {
		return nil, nil, err
	}

	cfg.BackOff = newLockWaitBackOff()

	connector, err := doltembed.NewConnector(cfg)
	if err != nil {
		return nil, nil, err
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	cleanup := func() error {
		dbErr := db.Close()
		connErr := connector.Close()
		// connector.Close → engine.Close → BackgroundThreads.Shutdown
		// always returns context.Canceled because Shutdown cancels its
		// own parent context then returns parentCtx.Err().  This is
		// a spurious error from a clean shutdown; filter it from each
		// result individually so real close errors are still surfaced.
		if errors.Is(dbErr, context.Canceled) {
			dbErr = nil
		}
		if errors.Is(connErr, context.Canceled) {
			connErr = nil
		}
		return errors.Join(dbErr, connErr)
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, cleanup())
	}

	if strings.TrimSpace(database) != "" {
		if !validIdentifier.MatchString(database) {
			msg := fmt.Sprintf("invalid database name: %q", database)
			if strings.ContainsRune(database, '-') {
				msg += "; hyphens are not allowed in embedded mode — replace with underscores in .beads/metadata.json dolt_database field, or run 'bd doctor'"
			}
			return nil, nil, errors.Join(errors.New(msg), cleanup())
		}
		if _, err := db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
			return nil, nil, errors.Join(err, cleanup())
		}
		if strings.TrimSpace(branch) != "" {
			if _, err := db.ExecContext(ctx, fmt.Sprintf("SET @@%s_head_ref = %s", database, sqlStringLiteral(branch))); err != nil {
				return nil, nil, errors.Join(err, cleanup())
			}
		}
	}

	return db, cleanup, nil
}

// newLockWaitBackOff returns the retry policy an engine open uses while another
// process holds the lock.
//
// With the library defaults (500ms initial, +-50%), a second bd that collided
// with a holder slept 250-750ms before its first retry even when the holder
// let go 10ms later, which bv saves and parallel agents hit. Each retry is a
// lock attempt, so short intervals are cheap. Timing only: the wait still has
// no elapsed-time limit.
func newLockWaitBackOff() *backoff.ExponentialBackOff {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = lockWaitInitialInterval
	bo.MaxInterval = lockWaitMaxInterval
	bo.MaxElapsedTime = 0 // wait until ctx cancellation
	bo.Reset()
	return bo
}

func buildDSN(dir, database string) string {
	v := url.Values{}
	v.Set(doltembed.CommitNameParam, commitName)
	v.Set(doltembed.CommitEmailParam, commitEmail)
	v.Set(doltembed.MultiStatementsParam, "true")
	if strings.TrimSpace(database) != "" {
		v.Set(doltembed.DatabaseParam, database)
	}
	// Build the DSN string manually instead of using url.URL.String(),
	// which percent-encodes the path (spaces → %20). The embedded Dolt
	// driver's ParseDataSource strips the "file://" prefix and uses the
	// remainder as a literal filesystem path, so encoding breaks paths
	// that contain spaces. See #2920.
	path := dir
	if os.PathSeparator == '\\' {
		path = strings.ReplaceAll(path, `\`, `/`)
	}
	return "file://" + path + "?" + v.Encode()
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(s), "'", "''") + "'"
}
