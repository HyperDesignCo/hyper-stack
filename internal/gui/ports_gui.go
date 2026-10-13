//go:build windows || (darwin && cgo)

package gui

import (
	"sync"

	"github.com/hyperdesign/hyper-stack/internal/doctor"
)

// suggestPorts keeps the given ports unless something else (Laragon, XAMPP, another
// MySQL) already listens on them, in which case it proposes the usual alternatives
// so the install does not fail on its first check.
func suggestPorts(httpP, httpsP, mysqlP int) (h, s, m int, changed bool) {
	type pick struct {
		port *int
		alt  int
	}
	h, s, m = httpP, httpsP, mysqlP
	picks := []pick{{&h, 8080}, {&s, 8443}, {&m, 3307}}
	var wg sync.WaitGroup
	busy := make([]bool, len(picks))
	for i, p := range picks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, busy[i] = doctor.PortOwner(*p.port)
		}()
	}
	wg.Wait()
	for i, p := range picks {
		if busy[i] {
			*p.port = p.alt
			changed = true
		}
	}
	return h, s, m, changed
}
