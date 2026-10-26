package contract

// LegacyAdminLabel is the management label used by Outlets set up before
// labels were generated. Those installs keep it so Connectors and bookmarks work.
const LegacyAdminLabel = "meerkat"

// AdminHostname is the public management endpoint for an Outlet domain.
func AdminHostname(label, domain string) string { return label + "." + domain }
