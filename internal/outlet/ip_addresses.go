package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/lawnect/meerkat/internal/control"
)

// parseRanges reads normalized ranges and addresses as prefixes; anything
// else is skipped.
func parseRanges(ranges []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ranges))
	for _, entry := range ranges {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, prefix)
		} else if addr, err := netip.ParseAddr(entry); err == nil {
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}

// scopePrefixes are the client addresses tags and ranges name: the ranges,
// and every address book entry carrying one of the tags. A tag no entry
// carries adds nothing.
func scopePrefixes(book control.IPAddresses, tags, ranges []string) []netip.Prefix {
	all := slices.Clone(ranges)
	for _, tag := range tags {
		all = append(all, book.Tagged(tag)...)
	}
	return parseRanges(all)
}

func prefixesContain(prefixes []netip.Prefix, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return slices.ContainsFunc(prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(addr) })
}

// ipAddressBook reads the address book; one that can't be read is empty,
// so its tags match no address, and damaged says so.
func (s *Store) ipAddressBook(ctx context.Context) (book control.IPAddresses, damaged bool, err error) {
	book, err = s.control.IPAddresses(ctx)
	if errors.Is(err, control.ErrStoredIPAddresses) {
		return book, true, nil
	}
	return book, false, err
}

type ipTagUse struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	// Policies are the names of the access policies naming the tag, and
	// AdminAccess whether the admin console limit does.
	Policies    []string `json:"policies"`
	AdminAccess bool     `json:"admin_access"`
}

type ipAddressesView struct {
	Entries []control.IPAddress `json:"entries"`
	Tags    []ipTagUse          `json:"tags"`
	// Damaged says the stored address book couldn't be read.
	Damaged bool `json:"damaged"`
}

func (s *Store) ipAddressesView(ctx context.Context) (ipAddressesView, error) {
	book, err := s.control.IPAddresses(ctx)
	damaged := errors.Is(err, control.ErrStoredIPAddresses)
	if err != nil && !damaged {
		return ipAddressesView{}, err
	}
	view := ipAddressesView{Entries: book.Entries, Tags: []ipTagUse{}, Damaged: damaged}
	index := map[string]int{}
	for _, e := range book.Entries {
		for _, tag := range e.Tags {
			key := strings.ToLower(tag)
			if i, ok := index[key]; ok {
				view.Tags[i].Count++
				continue
			}
			index[key] = len(view.Tags)
			view.Tags = append(view.Tags, ipTagUse{Name: tag, Count: 1, Policies: []string{}})
		}
	}
	if policies, err := s.control.AccessPolicies(ctx); err == nil || errors.Is(err, control.ErrStoredAccessPolicies) {
		for _, policy := range policies.Policies {
			for _, tag := range policy.IPTags {
				if i, ok := index[strings.ToLower(tag)]; ok && !slices.Contains(view.Tags[i].Policies, policy.Name) {
					view.Tags[i].Policies = append(view.Tags[i].Policies, policy.Name)
				}
			}
		}
	}
	if admin, err := s.control.AdminAccess(ctx); err == nil {
		for _, tag := range admin.AllowedTags {
			if i, ok := index[strings.ToLower(tag)]; ok {
				view.Tags[i].AdminAccess = true
			}
		}
	}
	slices.SortStableFunc(view.Tags, func(a, b ipTagUse) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return view, nil
}

func (s *Store) getIPAddresses(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	view, err := s.ipAddressesView(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

// changeIPAddress saves one address book change, refusing from the internet
// one that would shut the one saving out of the admin console, and applies
// it to the access policies and the console limit at once.
func (s *Store) changeIPAddress(w http.ResponseWriter, r *http.Request, action string, prospective func(control.IPAddresses) control.IPAddresses, save func() (control.IPAddress, error), created bool) {
	s.saving.Lock()
	defer s.saving.Unlock()
	if ip, public := requestOrigin(r); public {
		book, _, err := s.ipAddressBook(r.Context())
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		admin, err := s.control.AdminAccess(r.Context())
		if err != nil && !errors.Is(err, control.ErrDamagedAdminAccess) {
			fail(w, r, 503, "persistence_failed")
			return
		}
		// Refused when the limit lets this address in now and wouldn't after,
		// whether or not MEERKAT_ADMIN_IP_LIMIT lifts it, as saving the limit
		// itself is.
		if admin.Restricted && len(admin.AllowedTags) != 0 &&
			compileAdminAccess(admin, book).allows(ip) && !compileAdminAccess(admin, prospective(book)).allows(ip) {
			fail(w, r, 409, "admin_access_locks_out")
			return
		}
	}
	saved, err := save()
	if err == nil {
		// Saved: it is audited, and both reloads run even if one fails.
		s.audited(r, action, "ip_address", saved.ID, map[string]any{"range": saved.Range, "note": saved.Note, "tags": saved.Tags})
		ctx := context.WithoutCancel(r.Context())
		err = errors.Join(s.reloadAccess(ctx), s.reloadAdminAccess(ctx))
		if err != nil {
			slog.Error("an IP address book change was saved but not applied", "error", err)
		}
	}
	switch {
	case errors.Is(err, control.ErrInvalidIPAddress):
		fail(w, r, 400, "invalid_ip_address")
	case errors.Is(err, control.ErrIPAddressTaken):
		fail(w, r, 409, "ip_address_taken")
	case errors.Is(err, control.ErrIPAddressesFull):
		fail(w, r, 409, "ip_addresses_full")
	case errors.Is(err, control.ErrIPTagInUse):
		fail(w, r, 409, "ip_tag_in_use")
	case errors.Is(err, control.ErrIPAddressNotFound):
		fail(w, r, 404, "ip_address_not_found")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		if created {
			reply(w, 201, saved)
		} else if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			reply(w, 200, saved)
		}
	}
}

func (s *Store) createIPAddress(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.IPAddress
	if !decode(w, r, &req, "range", "note", "tags") {
		return
	}
	normalized, ok := req.Normalize()
	if !ok {
		fail(w, r, 400, "invalid_ip_address")
		return
	}
	s.changeIPAddress(w, r, "security.ip_address_create",
		func(book control.IPAddresses) control.IPAddresses { return book.With(normalized) },
		func() (control.IPAddress, error) { return s.control.CreateIPAddress(r.Context(), req, s.now()) }, true)
}

func (s *Store) updateIPAddress(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	var req control.IPAddress
	if !decode(w, r, &req, "range", "note", "tags") {
		return
	}
	normalized, ok := req.Normalize()
	if !ok {
		fail(w, r, 400, "invalid_ip_address")
		return
	}
	normalized.ID = id
	s.changeIPAddress(w, r, "security.ip_address_update",
		func(book control.IPAddresses) control.IPAddresses { return book.With(normalized) },
		func() (control.IPAddress, error) { return s.control.UpdateIPAddress(r.Context(), id, req, s.now()) }, false)
}

func (s *Store) deleteIPAddress(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	s.changeIPAddress(w, r, "security.ip_address_delete",
		func(book control.IPAddresses) control.IPAddresses { return book.Without(id) },
		func() (control.IPAddress, error) { return s.control.DeleteIPAddress(r.Context(), id, s.now()) }, false)
}
