package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type lease struct {
	uid     string
	expires time.Time
	active  map[uint64]func()
}
type leases struct {
	mu     sync.Mutex
	values map[string]*lease
	next   uint64
	ttl    time.Duration
}

func newLeases(ttl time.Duration) *leases { return &leases{values: map[string]*lease{}, ttl: ttl} }
func (l *leases) renew(key, uid string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if v := l.values[key]; v != nil && v.uid == uid && now.Before(v.expires) {
		v.expires = now.Add(l.ttl)
		return key, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key = hex.EncodeToString(b)
	l.values[key] = &lease{uid: uid, expires: now.Add(l.ttl), active: map[uint64]func(){}}
	return key, nil
}
func (l *leases) valid(key, uid string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.values[key]
	return v != nil && v.uid == uid && time.Now().Before(v.expires)
}
func (l *leases) attach(key, uid string, close func()) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.values[key]
	if v == nil || v.uid != uid || !time.Now().Before(v.expires) {
		return func() {}, false
	}
	l.next++
	id := l.next
	v.active[id] = close
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if v := l.values[key]; v != nil {
			delete(v.active, id)
		}
	}, true
}
func (l *leases) expire(all bool) {
	l.mu.Lock()
	var close []func()
	now := time.Now()
	for key, v := range l.values {
		if all || !now.Before(v.expires) {
			for _, f := range v.active {
				close = append(close, f)
			}
			delete(l.values, key)
		}
	}
	l.mu.Unlock()
	for _, f := range close {
		f()
	}
}
