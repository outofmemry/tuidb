package web

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestReviewDoubleClose(t *testing.T) {
	e := newEnv(t, "")
	logw := &lockedBuf{}
	e.srv.Config.ErrorLog = log.New(logw, "", 0)
	panics := 0
	for i := 0; i < 500; i++ {
		id := e.open()
		var wg sync.WaitGroup
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/sessions/"+id, nil)
				req.Header.Set("Authorization", "Bearer tok")
				if resp, err := http.DefaultClient.Do(req); err == nil {
					resp.Body.Close()
				}
			}()
		}
		wg.Wait()
	}
	panics = strings.Count(logw.String(), "close of closed channel")
	t.Logf("panics=%d log=%s", panics, logw.String())
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *lockedBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
