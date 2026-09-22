package steam

import (
	"context"

	"github.com/pkg/errors"
)

// Authenticate exchanges credentials and Steam Guard confirmation for a token.
// It does not open a CM connection. The returned token is a secret; store it in
// protected storage and pass it as LogOnDetails.AccessToken for later logins.
func (a *Auth) Authenticate(ctx context.Context, details *LogOnDetails) (string, error) {
	if details == nil || details.Username == "" || details.Password == "" {
		return "", errors.New("Steam username and password are required")
	}
	return a.getAccessTokenViaCredentials(ctx, details)
}
