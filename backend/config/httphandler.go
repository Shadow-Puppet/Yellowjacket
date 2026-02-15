package config

import (
	"fmt"
	"net/http"

	"github.com/gorilla/schema"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
)

var formDecoder = schema.NewDecoder()

func (c *Config) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.serveMux.ServeHTTP(w, r)
}

func (c *Config) handle(w http.ResponseWriter, r *http.Request) {
	c.logger.Debug("handling request from config http handler")

	switch r.Method {
	case http.MethodGet:
		if err := c.form().Render(r.Context(), w); err != nil {
			c.logger.Error("problem getting config html", "err", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
		}
	case http.MethodPost:
		if err := c.handleConfigPost(r); err != nil {
			c.logger.Error("problem handling config post request", "err", err.Error())

			renderErr := c.formSubmitError(err.Error()).Render(r.Context(), w)
			if renderErr != nil {
				c.logger.Error("problem rendering error response", "err", renderErr.Error())
			}

			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if err := c.formSubmitSuccess().Render(r.Context(), w); err != nil {
			c.logger.Error("problem rendering success response", "err", err.Error())
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (c *Config) handleConfigPost(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("could not parse form data: %w", err)
	}

	var postedConfig Config

	err := formDecoder.Decode(&postedConfig, r.PostForm)
	if err != nil {
		return fmt.Errorf("could not decode form data: %w", err)
	}

	c.logger.Debug("decoded config post form data", "postedConfig", postedConfig)

	// Update local config and emit event for listeners
	if postedConfig.Library != nil {
		c.Library = postedConfig.Library

		if c.ctx != nil {
			runtime.EventsEmit(c.ctx, events.LibraryConfigChanged, map[string]any{
				"DirectoryPath": string(c.Library.DirectoryPath),
			})
		}
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf("could not save posted config: %w", err)
	}

	return nil
}
