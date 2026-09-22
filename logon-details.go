package steam

import (
	"context"

	"github.com/paralin/go-steam/protocol/protobuf/unified"
)

// LogOnDetails supplies credentials or a reusable authentication token.
type LogOnDetails struct {
	// Username is the Steam account login name.
	Username string

	// Password starts a modern Steam auth session and exchanges credentials for
	// a token before CM logon.
	Password string

	// AccessToken logs on to CM directly when the caller already owns a modern
	// Steam authentication token.
	AccessToken string

	// DeviceFriendlyName labels the device in the modern Steam auth session.
	DeviceFriendlyName string

	// AuthCode supplies a Steam Guard email code when Steam requests one.
	AuthCode string

	// TwoFactorCode supplies a Steam Guard mobile authenticator code when Steam
	// requests one.
	TwoFactorCode string

	// ConfirmSteamGuard prompts within the existing authentication session.
	// Return a supplied code and its type, or a confirmation type with no code
	// after asking the person to approve the request in Steam or email.
	ConfirmSteamGuard func(context.Context, []SteamGuardConfirmation) (string, unified.EAuthSessionGuardType, error)

	// ShouldRememberPassword requests a persistent login token.
	ShouldRememberPassword bool
}
