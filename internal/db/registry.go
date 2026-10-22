package db

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu      sync.RWMutex
	drivers = map[string]Driver{}
)

// Register is called from each driver package's init(); main blank-imports them.
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	drivers[d.Name()] = d
}

func Lookup(name string) (Driver, error) {
	mu.RLock()
	defer mu.RUnlock()
	if d, ok := drivers[name]; ok {
		return d, nil
	}
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("no driver %q compiled in (have %v)", name, names)
}
