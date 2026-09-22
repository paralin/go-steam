package steam

import (
	"context"
	"net/http"
	"testing"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	"github.com/paralin/go-steam/protocol/protobuf/unified"
)

// TestInteractiveGuardRetainsSession submits the prompt's code to the original challenge.
func TestInteractiveGuardRetainsSession(t *testing.T) {
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, request *http.Request) protobuf.Message {
		if name != "UpdateAuthSessionWithSteamGuardCode" {
			t.Fatalf("prompt restarted authentication: %s", name)
		}
		message := &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{}
		readAuthRequest(t, request, message)
		if message.GetClientId() != 42 || message.GetCode() != "12345" {
			t.Fatal("confirmation did not retain the original session and code")
		}
		return &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response{}
	})
	t.Cleanup(server.Close)
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.submitAvailableSteamGuardCode(context.Background(), &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
		ClientId: uint64Ptr(42), Steamid: uint64Ptr(76561198000000000),
		AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{{ConfirmationType: guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode)}},
	}, &LogOnDetails{ConfirmSteamGuard: func(context.Context, []SteamGuardConfirmation) (string, unified.EAuthSessionGuardType, error) {
		return "12345", unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}
