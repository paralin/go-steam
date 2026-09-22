package steam

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/aperturerobotics/fastjson"
	"github.com/paralin/go-steam/netutil"
	"github.com/pkg/errors"
)

// InitializeSteamDirectory fetches Steam's CM directory within fifteen seconds.
func InitializeSteamDirectory() error { return steamDirectoryCache.Initialize() }

// steamDirectoryCache shares discovered addresses across authenticated clients.
var steamDirectoryCache = &steamDirectory{}

// steamDirectory guards the current CM address list.
type steamDirectory struct {
	// mtx guards servers; network I/O never holds this lock.
	mtx sync.RWMutex
	// servers contains parsed addresses from the latest successful discovery.
	servers []*netutil.PortAddr
}

// Initialize fetches the directory with the default bounded discovery lifetime.
func (sd *steamDirectory) Initialize() error { return sd.InitializeContext(context.Background()) }

// InitializeContext fetches and validates Steam's public CM address list.
func (sd *steamDirectory) InitializeContext(ctx context.Context) error {
	// Bound discovery independently from the lifetime of a connected Steam client.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.steampowered.com/ISteamDirectory/GetCMList/v1/?cellId=0", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(body) > 1024*1024 {
		return errors.New("Steam directory response exceeds one MiB")
	}
	if response.StatusCode != http.StatusOK {
		return errors.Errorf("got non-200 response from Steam: status=%d body=%s", response.StatusCode, string(body))
	}

	// Parse actual addresses before replacing the last useful directory snapshot.
	var parser fastjson.Parser
	value, err := parser.ParseBytes(body)
	if err != nil {
		return err
	}
	if value.GetUint("response", "result") != 1 {
		return errors.New("Steam directory request failed")
	}
	var servers []*netutil.PortAddr
	for _, entry := range value.GetArray("response", "serverlist") {
		address, err := entry.StringBytes()
		if err != nil {
			return err
		}
		parsed := netutil.ParsePortAddr(string(address))
		if parsed == nil {
			return errors.New("Steam directory returned an invalid address")
		}
		servers = append(servers, parsed)
	}
	if len(servers) == 0 {
		return errors.New("Steam directory returned no servers")
	}
	sd.mtx.Lock()
	sd.servers = servers
	sd.mtx.Unlock()
	return nil
}

// GetRandomCM returns a parsed address from an initialized directory.
func (sd *steamDirectory) GetRandomCM() *netutil.PortAddr {
	sd.mtx.RLock()
	defer sd.mtx.RUnlock()
	return sd.servers[rand.IntN(len(sd.servers))]
}

// IsInitialized reports whether discovery has supplied usable CM addresses.
func (sd *steamDirectory) IsInitialized() bool {
	sd.mtx.RLock()
	defer sd.mtx.RUnlock()
	return len(sd.servers) != 0
}
