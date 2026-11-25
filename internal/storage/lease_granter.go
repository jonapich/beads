package storage

// RemoteLeaseGranter is implemented by a store whose claims and heartbeats run
// in another process that grants the lease length itself. The client's context
// does not cross the wire, so a lease length put on it is never seen; a caller
// that would otherwise read lease.ttl for the sake of that context (bd's root
// pre-run) can skip the read, and the request it costs.
//
// A store answers true only when the lease is the other side's to length, which
// is the case for the HTTP backend: bd serve reads lease.ttl for the claims,
// ready-claims, claiming updates and heartbeats it executes.
type RemoteLeaseGranter interface {
	GrantsLeasesRemotely() bool
}

// GrantsLeasesRemotely reports whether st (looking through decorators) is a
// RemoteLeaseGranter that answers true. A nil store, and every store that runs
// its own claims, answer false.
func GrantsLeasesRemotely(st DoltStorage) bool {
	if st == nil {
		return false
	}
	g, ok := UnwrapStore(st).(RemoteLeaseGranter)
	return ok && g.GrantsLeasesRemotely()
}
