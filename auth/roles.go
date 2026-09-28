package auth

// Roles stored in users.role and carried in access token claims.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Provider identifiers stored in auth_providers.provider. The social ones are
// also the :provider path segment of the OAuth routes.
const (
	ProviderEmail  = "email"
	ProviderGoogle = "google"
	ProviderGitHub = "github"
	ProviderApple  = "apple"
)
