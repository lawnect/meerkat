package outlet

import (
	"context"
	"net/http"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	hstsHeader = "Strict-Transport-Security"
	// hstsValue is a year, the management host's too, without
	// includeSubDomains: only the address itself is held to HTTPS.
	hstsValue = "max-age=31536000"
)

func (s *Store) reloadHSTS(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	h, err := s.control.HSTS(ctx)
	if err != nil {
		return err
	}
	s.hsts.Store(h.Enabled)
	return nil
}

// setHSTS adds the header to an HTTPS answer on a public address when the
// setting is on: the Outlet's own answers. A proxied app's response gets
// it from applyHSTS.
func (s *Store) setHSTS(w http.ResponseWriter) {
	if s.hsts.Load() {
		w.Header().Set(hstsHeader, hstsValue)
	}
}

// applyHSTS moves the header onto the app's response itself, unless the
// app sent its own (max-age=0 included), which is then the only one
// browsers see. One left on w would be lost: the proxy clears w's headers
// after relaying an informational answer such as 103 Early Hints or 100
// Continue.
func (s *Store) applyHSTS(w http.ResponseWriter, res *http.Response) {
	w.Header().Del(hstsHeader)
	if s.hsts.Load() && len(res.Header.Values(hstsHeader)) == 0 {
		res.Header.Set(hstsHeader, hstsValue)
	}
}

func (s *Store) getHSTS(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	h, err := s.control.HSTS(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, h)
}

func (s *Store) putHSTS(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.HSTS
	if !decode(w, r, &req, "enabled") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetHSTS(r.Context(), req, s.now())
	if err == nil {
		err = s.reloadHSTS(context.WithoutCancel(r.Context()))
	}
	s.saving.Unlock()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.hsts", "security", "hsts", map[string]any{"enabled": saved.Enabled})
	reply(w, 200, saved)
}
