package handlers

// Handlers carries the dependencies for every HTTP handler method. Methods
// are spread across auth.go, auth_email.go, admin_users.go and settings.go by
// resource; there is one receiver type so wiring and tests have one thing to
// build.
type Handlers struct {
	Deps
}

// New returns the handler set for d.
func New(d Deps) *Handlers { return &Handlers{Deps: d} }
