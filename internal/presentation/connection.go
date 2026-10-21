package presentation

// Connection is display metadata shared by credential configuration and the
// translation provider picker. It never contains a stored secret value.
type Connection struct {
	ID                                            string
	DisplayName, DocsURL, Endpoint, CredentialKey string
	NeedsCredential, Configured, SupportsImage    bool
}
