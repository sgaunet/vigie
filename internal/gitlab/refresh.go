package gitlab

import (
	"errors"

	"github.com/sgaunet/vigie/internal/status"
)

// RefreshError returns the error that makes a whole refresh fail: a rejected token, or a
// network error on every field (nothing usable was fetched). It is nil otherwise.
func RefreshError(fields []status.FieldResult) error {
	allNetwork := len(fields) > 0
	var first error
	for _, f := range fields {
		switch {
		case f.Err == nil:
			allNetwork = false
			continue
		case errors.Is(f.Err, ErrUnauthorized):
			return f.Err
		}
		if !errors.Is(f.Err, ErrNetwork) {
			allNetwork = false
		}
		if first == nil {
			first = f.Err
		}
	}
	if allNetwork {
		return first
	}
	return nil
}
