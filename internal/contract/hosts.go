package contract

// AdminHostname is the public management endpoint for an Outlet domain.
func AdminHostname(domain string) string { return "meerkat." + domain }
