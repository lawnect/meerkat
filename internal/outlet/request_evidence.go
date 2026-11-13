package outlet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/lawnect/meerkat/internal/eventlog"
)

const maxEvidencePath = 512

// requestEvidence is captured once before decisions or proxy rewrites. It
// deliberately excludes query strings, headers, cookies and credentials.
type requestEvidence struct {
	RequestID            string `json:"request_id"`
	EscapedPath          string `json:"escaped_path"`
	PathTruncated        bool   `json:"path_truncated"`
	EscapedPathTruncated bool   `json:"escaped_path_truncated"`
}

var evidenceColumns = []string{"request_id TEXT DEFAULT ''", "escaped_path TEXT DEFAULT ''", "path_truncated INTEGER DEFAULT 0", "escaped_path_truncated INTEGER DEFAULT 0"}

func (e *requestEvidence) fields() []any {
	return []any{&e.RequestID, &e.EscapedPath, &e.PathTruncated, &e.EscapedPathTruncated}
}

func evidenceOf(r *http.Request) requestEvidence {
	if record := recordOf(r); record != nil {
		return record.evidence
	}
	return requestEvidence{}
}

// evidenceRequest creates a record even with the request log off: access and
// failure entries still need the same server-generated correlation ID.
func (s *Store) evidenceRequest(r *http.Request) *http.Request {
	if recordOf(r) != nil || !s.logs.requests.on() && !s.logs.access.on() && !s.logs.failures.on() {
		return r
	}
	path := r.URL.Path[:min(len(r.URL.Path), maxEvidencePath)]
	pathTruncated := len(strings.ToValidUTF8(r.URL.Path, "\uFFFD")) > maxEvidencePath
	var escaped string
	var truncated bool
	if r.URL.RawPath == "" {
		// Escape a bounded prefix so an early oversized-path refusal does
		// not allocate another copy of the whole attacker-controlled path.
		escaped = (&url.URL{Path: path}).EscapedPath()
		truncated = len(path) < len(r.URL.Path)
	} else if len(r.URL.Path) <= maxPublicPath && len(r.URL.RawPath) <= 3*maxPublicPath {
		escaped = r.URL.EscapedPath()
	} else {
		// net/http retains the original escaped request target. Only an
		// origin-form path is used; the query is always discarded first.
		escaped, _, _ = strings.Cut(r.RequestURI, "?")
		if !strings.HasPrefix(escaped, "/") {
			escaped = (&url.URL{Path: path}).EscapedPath()
		}
		truncated = true
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	evidence := requestEvidence{RequestID: hex.EncodeToString(id[:]), EscapedPath: eventlog.Clip(escaped, maxEvidencePath),
		PathTruncated: pathTruncated, EscapedPathTruncated: truncated || len(escaped) > maxEvidencePath}
	record := &requestRecord{evidence: evidence}
	return r.WithContext(context.WithValue(r.Context(), requestRecordKey{}, record))
}
