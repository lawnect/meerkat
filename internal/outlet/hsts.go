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
// setting is on. A proxied app that sends its own keeps it (see
// keepAppHSTS).
func (s *Store) setHSTS(w http.ResponseWriter) {
	if s.hsts.Load() {
		w.Header().Set(hstsHeader, hstsValue)
	}
}

// keepAppHSTS drops the Outlet's header when the app's response has one,
// max-age=0 included, so the app's choice is the only one browsers see.
func keepAppHSTS(w http.ResponseWriter, res *http.Response) {
	if len(res.Header.Values(hstsHeader)) != 0 {
		w.Header().Del(hstsHeader)
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
