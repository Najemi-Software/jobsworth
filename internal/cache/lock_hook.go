package cache

// LockContended, when set, is called each time AcquireLock finds the lock
// held and is about to wait. Tests use it as the readiness signal that a
// waiter is blocked (test spec §4.1: no sleeps).
var LockContended func()
