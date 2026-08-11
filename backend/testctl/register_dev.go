//go:build dev

package testctl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
)

// Static errors — err113 forbids fmt.Errorf with a dynamic message at
// the point of failure, and these are all conditions a caller may want
// to match on anyway.
var (
	errBadName      = errors.New("name must match [A-Za-z0-9_-]{1,64}")
	errNoSnapshot   = errors.New("no such snapshot")
	errNoEventName  = errors.New("emit needs a non-empty name")
	errNoSQL        = errors.New("sql must be non-empty")
	errBadBody      = errors.New("request body is not valid JSON")
	errInconsistent = errors.New(
		"restore left foreign key violations")
)

// safeName keeps snapshot names to something that cannot escape the
// snapshot directory or surprise a shell.
var safeName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Register mounts the control surface, if and only if this is a dev
// build *and* YJ_TESTCTL=1.  A human running `make dev` gets neither
// the routes nor the risk.
func Register(r Registrar, d Deps) {
	if os.Getenv(EnvEnable) != "1" {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /__test/health", jsonHandler(d, handleHealth))
	mux.HandleFunc("POST /__test/db/snapshot", jsonHandler(d, handleSnapshot))
	mux.HandleFunc("POST /__test/db/restore", jsonHandler(d, handleRestore))
	mux.HandleFunc("POST /__test/emit", jsonHandler(d, handleEmit))
	mux.HandleFunc("POST /__test/sql", jsonHandler(d, handleSQL))

	r.RegisterHandler(Prefix, mux)

	d.Logger.Warn(
		"test control surface enabled — dev build with YJ_TESTCTL=1",
		"prefix", Prefix,
	)
}

// handlerFunc is the shape every endpoint has: take the request,
// return something JSON-encodable or an error.
type handlerFunc func(Deps, *http.Request) (any, error)

// jsonHandler centralises encoding, status codes and logging so each
// endpoint is only its own logic.  A failure is a 400 with the reason
// in the body — an agent reading a spec failure needs the reason, not
// a bare status code.
func jsonHandler(d Deps, fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := fn(d, r)

		w.Header().Set("Content-Type", "application/json")

		if err != nil {
			d.Logger.Error("testctl request failed",
				"path", r.URL.Path, "err", err.Error())
			w.WriteHeader(http.StatusBadRequest)

			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": err.Error(),
			})

			return
		}

		_ = json.NewEncoder(w).Encode(result)
	}
}

// decode reads a JSON request body into v.  An empty body is not an
// error: several endpoints take everything in the query string.
func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}

	dec := json.NewDecoder(r.Body)

	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return errBadBody
	}

	return nil
}
