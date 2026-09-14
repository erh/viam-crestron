package component

import (
	"os"
	"sync"

	"github.com/erh/viam-crestron/crestron"
)

// The module often runs many light and scene components pointing at the same
// processor. They share one *crestron.Client per host+token so there is a
// single session (with its own re-login handling) rather than one per resource.
var (
	clientMu    sync.Mutex
	clientCache = map[string]*crestron.Client{}
)

func resolveHost(host string) string {
	if host != "" {
		return host
	}
	if h := os.Getenv("CRESTRON_HOST"); h != "" {
		return h
	}
	return "192.168.3.2"
}

func resolveToken(token string) string {
	if token != "" {
		return token
	}
	crestron.LoadEnvFile()
	return os.Getenv("CRESTRON_TOKEN")
}

// sharedClient returns a cached client for the resolved host+token.
func sharedClient(host, token string) *crestron.Client {
	h, t := resolveHost(host), resolveToken(token)
	key := h + "\x00" + t
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clientCache[key]; ok {
		return c
	}
	c := crestron.New(h, t)
	clientCache[key] = c
	return c
}
