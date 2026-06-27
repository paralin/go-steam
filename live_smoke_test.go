package steam_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	steam "github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
)

func TestLiveSteamCredentialSmoke(t *testing.T) {
	username := os.Getenv("STEAM_SMOKE_USERNAME")
	password := os.Getenv("STEAM_SMOKE_PASSWORD")
	if username == "" || password == "" {
		t.Skip("STEAM_SMOKE_USERNAME and STEAM_SMOKE_PASSWORD must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := steam.NewClient()
	defer client.Disconnect()

	client.Connect()
	t.Log("Steam connection requested")

	loggedOn := false
	webSession := false
	webLoggedOn := false
	accountInfo := false

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("live smoke timed out: loggedOn=%t webSession=%t webLoggedOn=%t accountInfo=%t", loggedOn, webSession, webLoggedOn, accountInfo)
		case event := <-client.Events():
			switch event := event.(type) {
			case *steam.ConnectedEvent:
				t.Log("connected to Steam CM; starting credential auth")
				if err := client.Auth.LogOn(ctx, &steam.LogOnDetails{
					Username:           username,
					Password:           password,
					DeviceFriendlyName: "go-steam-live-smoke",
					AuthCode:           os.Getenv("STEAM_SMOKE_AUTH_CODE"),
					TwoFactorCode:      os.Getenv("STEAM_SMOKE_TFA_CODE"),
				}); err != nil {
					var authErr *steam.AuthSessionError
					if errors.As(err, &authErr) {
						t.Fatalf("credential auth failed before CM logon: state=%s confirmations=%s", authErr.State, confirmationTypes(authErr.Confirmations))
					}
					t.Fatalf("credential auth failed before CM logon: %v", err)
				}
			case *steam.LoggedOnEvent:
				if event.Result != steamlang.EResult_OK {
					t.Fatalf("CM logon failed: result=%v extended=%v", event.Result, event.ExtendedResult)
				}
				loggedOn = true
				t.Log("CM logon succeeded")
			case *steam.LogOnFailedEvent:
				if event.Err != nil {
					t.Fatalf("CM logon failed before access token: state=%s err=%v", event.AuthSessionState, event.Err)
				}
				t.Fatalf("CM logon failed: result=%v state=%s", event.Result, event.AuthSessionState)
			case *steam.SteamFailureEvent:
				t.Fatalf("Steam failure during smoke: result=%v", event.Result)
			case steam.FatalErrorEvent:
				t.Fatalf("fatal client error: %v", error(event))
			case error:
				t.Fatalf("client error: %v", event)
			case *steam.WebSessionIdEvent:
				webSession = true
				t.Log("web session nonce received; starting web logon")
				client.Web.LogOn()
			case *steam.WebLoggedOnEvent:
				webLoggedOn = true
				t.Log("web logon succeeded")
				if client.Web.SteamLogin == "" || client.Web.SteamLoginSecure == "" {
					t.Fatal("web logon event emitted without SteamLogin cookies")
				}
			case steam.WebLogOnErrorEvent:
				t.Fatalf("web logon failed: %v", error(event))
			case *steam.AccountInfoEvent:
				accountInfo = true
				t.Log("account info received")
			}
			if loggedOn && accountInfo {
				t.Log("core live login milestones reached; setting persona online")
				client.Social.SetPersonaState(steamlang.EPersonaState_Online)
				return
			}
		}
	}
}

func confirmationTypes(confirmations []steam.SteamGuardConfirmation) string {
	if len(confirmations) == 0 {
		return "none"
	}
	types := make([]string, len(confirmations))
	for i, confirmation := range confirmations {
		types[i] = fmt.Sprint(confirmation.Type)
	}
	return fmt.Sprint(types)
}
