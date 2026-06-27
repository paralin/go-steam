package steam

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	protocol "github.com/paralin/go-steam/protocol"
	steampb "github.com/paralin/go-steam/protocol/protobuf"
	unified "github.com/paralin/go-steam/protocol/protobuf/unified"
	steamlang "github.com/paralin/go-steam/protocol/steamlang"
)

func TestLogOnWithAccessTokenWritesCMAccessToken(t *testing.T) {
	client := &Client{
		events:    make(chan interface{}, 1),
		conn:      testConnection{},
		writeChan: make(chan protocol.IMsg, 1),
	}
	auth := &Auth{client: client}

	if err := auth.LogOn(context.Background(), &LogOnDetails{
		Username:    "alice",
		AccessToken: "access-token",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-client.writeChan:
		clientMsg, ok := msg.(*protocol.ClientMsgProtobuf)
		if !ok {
			t.Fatalf("message = %T", msg)
		}
		body, ok := clientMsg.Body.(*steampb.CMsgClientLogon)
		if !ok {
			t.Fatalf("body = %T", clientMsg.Body)
		}
		if body.GetAccountName() != "alice" {
			t.Fatalf("account name = %q", body.GetAccountName())
		}
		if body.GetAccessToken() != "access-token" {
			t.Fatalf("access token = %q", body.GetAccessToken())
		}
		if body.GetPassword() != "" {
			t.Fatalf("password = %q", body.GetPassword())
		}
	default:
		t.Fatal("logon message was not written")
	}
}

func TestLogOnResponseRequestsWebAPINonce(t *testing.T) {
	client := &Client{
		events:    make(chan interface{}, 1),
		conn:      testConnection{},
		writeChan: make(chan protocol.IMsg, 1),
	}
	auth := &Auth{client: client}
	auth.handleLogOnResponse(clientLogOnResponsePacket(t))

	select {
	case event := <-client.events:
		if _, ok := event.(*LoggedOnEvent); !ok {
			t.Fatalf("event = %T", event)
		}
	default:
		t.Fatal("logged-on event was not emitted")
	}

	select {
	case msg := <-client.writeChan:
		clientMsg, ok := msg.(*protocol.ClientMsgProtobuf)
		if !ok {
			t.Fatalf("message = %T", msg)
		}
		if clientMsg.GetMsgType() != steamlang.EMsg_ClientRequestWebAPIAuthenticateUserNonce {
			t.Fatalf("message type = %v", clientMsg.GetMsgType())
		}
		if _, ok := clientMsg.Body.(*steampb.CMsgClientRequestWebAPIAuthenticateUserNonce); !ok {
			t.Fatalf("body = %T", clientMsg.Body)
		}
	default:
		t.Fatal("web api nonce request was not written")
	}
}

func TestGetAccessTokenViaCredentialsReturnsRefreshToken(t *testing.T) {
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			req := new(unified.CAuthentication_GetPasswordRSAPublicKey_Request)
			readAuthRequest(t, r, req)
			if req.GetAccountName() != "alice" {
				t.Fatalf("account name = %q", req.GetAccountName())
			}
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			req := new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request)
			readAuthRequest(t, r, req)
			if req.GetAccountName() != "alice" {
				t.Fatalf("begin account name = %q", req.GetAccountName())
			}
			if req.GetEncryptedPassword() == "" {
				t.Fatal("encrypted password is empty")
			}
			if req.GetEncryptionTimestamp() != 123 {
				t.Fatalf("encryption timestamp = %d", req.GetEncryptionTimestamp())
			}
			if !req.GetRememberLogin() {
				t.Fatal("remember login is false")
			}
			if req.GetDeviceFriendlyName() != "go-steam" {
				t.Fatalf("device friendly name = %q", req.GetDeviceFriendlyName())
			}
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
			}
		case "PollAuthSessionStatus":
			req := new(unified.CAuthentication_PollAuthSessionStatus_Request)
			readAuthRequest(t, r, req)
			if req.GetClientId() != 42 {
				t.Fatalf("poll client id = %d", req.GetClientId())
			}
			if string(req.GetRequestId()) != "request" {
				t.Fatalf("poll request id = %q", req.GetRequestId())
			}
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	token, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username:               "alice",
		Password:               "password",
		ShouldRememberPassword: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "refresh-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestAuthServiceCallReportsEresultAsDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-eresult", "5")
		w.Header().Set("x-error_message", "invalid password")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	err := auth.authServiceCall(
		context.Background(),
		http.MethodPost,
		"PollAuthSessionStatus",
		&unified.CAuthentication_PollAuthSessionStatus_Request{},
		&unified.CAuthentication_PollAuthSessionStatus_Response{},
	)
	var authErr *AuthSessionError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T", err)
	}
	if authErr.State != AuthSessionStateDenied {
		t.Fatalf("state = %q", authErr.State)
	}
}

func TestGetAccessTokenViaCredentialsReportsSteamGuard(t *testing.T) {
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{
						ConfirmationType:  guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode),
						AssociatedMessage: stringPtr("a***@example.com"),
					},
				},
			}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username: "alice",
		Password: "password",
	})
	var authErr *AuthSessionError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if authErr.State != AuthSessionStateEmailCode {
		t.Fatalf("state = %s", authErr.State)
	}
	if len(authErr.Confirmations) != 1 {
		t.Fatalf("confirmations = %d", len(authErr.Confirmations))
	}
	if authErr.Confirmations[0].Type != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode {
		t.Fatalf("confirmation type = %v", authErr.Confirmations[0].Type)
	}
}

func TestGetAccessTokenViaCredentialsSubmitsSteamGuardCode(t *testing.T) {
	updated := false
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{ConfirmationType: guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode)},
				},
			}
		case "UpdateAuthSessionWithSteamGuardCode":
			req := new(unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request)
			readAuthRequest(t, r, req)
			if req.GetCode() != "12345" {
				t.Fatalf("guard code = %q", req.GetCode())
			}
			if req.GetCodeType() != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode {
				t.Fatalf("guard code type = %v", req.GetCodeType())
			}
			updated = true
			return &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response{}
		case "PollAuthSessionStatus":
			readAuthRequest(t, r, new(unified.CAuthentication_PollAuthSessionStatus_Request))
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username:      "alice",
		Password:      "password",
		TwoFactorCode: "12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("steam guard update was not submitted")
	}
}

func TestGetAccessTokenViaCredentialsSubmitsEmailSteamGuardCode(t *testing.T) {
	updated := false
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{ConfirmationType: guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode)},
				},
			}
		case "UpdateAuthSessionWithSteamGuardCode":
			req := new(unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request)
			readAuthRequest(t, r, req)
			if req.GetCode() != "ABC123" {
				t.Fatalf("guard code = %q", req.GetCode())
			}
			if req.GetCodeType() != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode {
				t.Fatalf("guard code type = %v", req.GetCodeType())
			}
			updated = true
			return &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response{}
		case "PollAuthSessionStatus":
			readAuthRequest(t, r, new(unified.CAuthentication_PollAuthSessionStatus_Request))
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username: "alice",
		Password: "password",
		AuthCode: "ABC123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("steam guard update was not submitted")
	}
}

func TestGetAccessTokenViaCredentialsPollsManualSteamGuardConfirmation(t *testing.T) {
	for _, confirmationType := range []unified.EAuthSessionGuardType{
		unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation,
		unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailConfirmation,
	} {
		t.Run(confirmationType.String(), func(t *testing.T) {
			server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
				switch name {
				case "GetPasswordRSAPublicKey":
					readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
					return authRSAPublicKeyResponse(t)
				case "BeginAuthSessionViaCredentials":
					readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
					return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
						ClientId:  uint64Ptr(42),
						RequestId: []byte("request"),
						Steamid:   uint64Ptr(76561198000000000),
						AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
							{ConfirmationType: guardTypePtr(confirmationType)},
						},
					}
				case "PollAuthSessionStatus":
					req := new(unified.CAuthentication_PollAuthSessionStatus_Request)
					readAuthRequest(t, r, req)
					if req.GetClientId() != 42 {
						t.Fatalf("poll client id = %d", req.GetClientId())
					}
					if string(req.GetRequestId()) != "request" {
						t.Fatalf("poll request id = %q", req.GetRequestId())
					}
					return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
				case "UpdateAuthSessionWithSteamGuardCode":
					t.Fatal("manual confirmation should not submit a Steam Guard code")
					return nil
				default:
					t.Fatalf("unexpected auth method %q", name)
					return nil
				}
			})
			defer server.Close()

			auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
			token, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
				Username: "alice",
				Password: "password",
			})
			if err != nil {
				t.Fatal(err)
			}
			if token != "refresh-token" {
				t.Fatalf("token = %q", token)
			}
		})
	}
}

func clientLogOnResponsePacket(t *testing.T) *protocol.Packet {
	t.Helper()
	return protoBodyPacket(t, steamlang.EMsg_ClientLogOnResponse, &steampb.CMsgClientLogonResponse{
		Eresult:               int32Ptr(int32(steamlang.EResult_OK)),
		HeartbeatSeconds:      int32Ptr(2),
		ClientSuppliedSteamid: uint64Ptr(76561198000000000),
	})
}

func protoBodyPacket(t *testing.T, eMsg steamlang.EMsg, msg protobuf.Message) *protocol.Packet {
	t.Helper()
	buf := new(bytes.Buffer)
	body, err := msg.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(eMsg)|steamlang.ProtoMask); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(buf, binary.LittleEndian, int32(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := buf.Write(body); err != nil {
		t.Fatal(err)
	}
	return &protocol.Packet{
		EMsg:    eMsg,
		IsProto: true,
		Data:    buf.Bytes(),
	}
}

func newAuthServiceTestServer(t *testing.T, handle func(*testing.T, string, *http.Request) protobuf.Message) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 2 || parts[1] != "v1" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		writeAuthResponse(t, w, handle(t, parts[0], r))
	}))
}

func readAuthRequest(t *testing.T, r *http.Request, msg protobuf.Message) {
	t.Helper()
	encoded := r.URL.Query().Get("input_protobuf_encoded")
	if encoded == "" {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		encoded = r.Form.Get("input_protobuf_encoded")
	}
	if encoded == "" {
		t.Fatal("missing input_protobuf_encoded")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := msg.UnmarshalVT(data); err != nil {
		t.Fatal(err)
	}
}

func writeAuthResponse(t *testing.T, w http.ResponseWriter, msg protobuf.Message) {
	t.Helper()
	data, err := msg.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write(data)
	if err != nil {
		t.Fatal(err)
	}
}

func authRSAPublicKeyResponse(t *testing.T) *unified.CAuthentication_GetPasswordRSAPublicKey_Response {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return &unified.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: stringPtr(key.N.Text(16)),
		PublickeyExp: stringPtr(strconv.FormatInt(int64(key.E), 16)),
		Timestamp:    uint64Ptr(123),
	}
}

func uint32Ptr(value uint32) *uint32 {
	return &value
}

func int32Ptr(value int32) *int32 {
	return &value
}

func guardTypePtr(value unified.EAuthSessionGuardType) *unified.EAuthSessionGuardType {
	return &value
}

type testConnection struct{}

func (testConnection) Read() (*protocol.Packet, error) {
	return nil, nil
}

func (testConnection) Write([]byte) error {
	return nil
}

func (testConnection) Close() error {
	return nil
}

func (testConnection) SetEncryptionKey([]byte) {}

func (testConnection) IsEncrypted() bool {
	return false
}
