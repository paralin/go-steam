package web

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
)

// GetAppList collects every category in Valve's store directory.
// The caller's context bounds all pages; partial results accompany errors.
func (c *Client) GetAppList(ctx context.Context) ([]*App, error) {
	// Collect all product categories through one context-bound cursor sequence.
	filter := AppListFilter{IncludeDLC: true, IncludeSoftware: true, IncludeVideos: true, IncludeHardware: true, MaxResults: 50000}
	var apps []*App
	for {
		page, err := c.GetAppListPage(ctx, filter)
		if err != nil {
			return apps, err
		}

		// Retain successful pages and stop if the provider cannot advance.
		apps = append(apps, page.Apps...)
		if !page.HaveMoreResults {
			return apps, nil
		}
		if page.LastAppId <= filter.LastAppID {
			return apps, errors.New("Steam app directory pagination did not advance")
		}

		// Continue strictly after the provider's last returned app.
		filter.LastAppID = page.LastAppId
	}
}

// AppListFilter selects one page or an incremental refresh of the store directory.
type AppListFilter struct {
	// ModifiedSince limits results to changes after this Unix timestamp in seconds.
	ModifiedSince uint32
	// Language requires a description in the specified language.
	Language string
	// ExcludeGames removes games, which are included by default.
	ExcludeGames bool
	// IncludeDLC includes downloadable content.
	IncludeDLC bool
	// IncludeSoftware includes software applications.
	IncludeSoftware bool
	// IncludeVideos includes videos and series.
	IncludeVideos bool
	// IncludeHardware includes hardware products.
	IncludeHardware bool
	// LastAppID continues after the previous page's LastAppId.
	LastAppID uint32
	// MaxResults limits this page to at most 50,000 entries; zero uses Valve's default.
	MaxResults uint32
}

// GetAppListPage reads the current IStoreService directory using a Web API key.
func (c *Client) GetAppListPage(ctx context.Context, filter AppListFilter) (*AppList, error) {
	// Encode categories and incremental refresh constraints with Valve's page bound.
	values := url.Values{
		"if_modified_since":         {strconv.FormatUint(uint64(filter.ModifiedSince), 10)},
		"have_description_language": {filter.Language},
		"include_games":             {strconv.FormatBool(!filter.ExcludeGames)},
		"include_dlc":               {strconv.FormatBool(filter.IncludeDLC)},
		"include_software":          {strconv.FormatBool(filter.IncludeSoftware)},
		"include_videos":            {strconv.FormatBool(filter.IncludeVideos)},
		"include_hardware":          {strconv.FormatBool(filter.IncludeHardware)},
		"last_appid":                {strconv.FormatUint(uint64(filter.LastAppID), 10)},
	}
	if filter.MaxResults != 0 {
		values.Set("max_results", strconv.FormatUint(uint64(min(filter.MaxResults, 50000)), 10))
	}

	// Decode the product page and its cursor together.
	response, err := c.Call(ctx, http.MethodGet, "IStoreService", "GetAppList", 1, values)
	if err != nil {
		return nil, err
	}
	result := new(AppList)
	if err := Decode(response, result, "response"); err != nil {
		return nil, err
	}
	return result, nil
}

// AppVersion reports the current version and whether a requested build is current.
type AppVersion struct {
	// Current is Valve's required build version.
	Current uint32
	// UpToDate indicates that Valve accepts the requested build version.
	UpToDate bool
	// Listable indicates that servers running the version may be listed.
	Listable bool
}

// CheckAppVersion checks one application build against Valve's current version.
func (c *Client) CheckAppVersion(ctx context.Context, app, version uint32) (AppVersion, error) {
	// Ask Valve for the required version and listing policy.
	response, err := c.Call(ctx, http.MethodGet, "ISteamApps", "UpToDateCheck", 1, url.Values{"appid": {strconv.FormatUint(uint64(app), 10)}, "version": {strconv.FormatUint(uint64(version), 10)}})
	if err != nil {
		return AppVersion{}, err
	}

	// A provider failure must not appear as an out-of-date build.
	result := response.Get("response")
	if result == nil || !result.GetBool("success") {
		return AppVersion{}, errors.New("Steam app version lookup failed")
	}
	return AppVersion{Current: uint32(result.GetUint("required_version")), UpToDate: result.GetBool("up_to_date"), Listable: result.GetBool("version_is_listable")}, nil
}

// IsAppUpToDate reports whether Valve accepts the supplied application version.
func (c *Client) IsAppUpToDate(ctx context.Context, app, version uint32) (bool, error) {
	result, err := c.CheckAppVersion(ctx, app, version)
	return result.UpToDate, err
}

// GetCurrentAppVersion returns the build version currently required by Valve.
func (c *Client) GetCurrentAppVersion(ctx context.Context, app uint32) (uint32, error) {
	result, err := c.CheckAppVersion(ctx, app, 1)
	return result.Current, err
}

// GetServerInfo lists Steam servers advertising the given address.
func (c *Client) GetServerInfo(ctx context.Context, address netip.Addr) ([]*ServerInfo, error) {
	// Preserve a concrete address rather than accepting arbitrary URL parameters.
	response, err := c.Call(ctx, http.MethodGet, "ISteamApps", "GetServersAtAddress", 1, url.Values{"addr": {address.String()}})
	if err != nil {
		return nil, err
	}

	// Failed lookups remain distinguishable from an address with no servers.
	if !response.GetBool("response", "success") {
		return nil, errors.New("Steam server lookup failed")
	}
	return decodeList[ServerInfo, *ServerInfo](response, "response", "servers")
}
