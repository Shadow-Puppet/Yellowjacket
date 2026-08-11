//go:build !dev

package testctl

// Register is a no-op in non-dev builds: the control surface's entire
// implementation is behind the `dev` build tag, so a release binary
// contains neither the handlers nor the routes.
func Register(_ Registrar, _ Deps) {}
