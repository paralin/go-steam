package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/paralin/go-steam/steamid"
	"github.com/pkg/errors"
)

// GetPlayerItems returns an application's economy inventory for a Steam user.
func (c *Client) GetPlayerItems(ctx context.Context, id steamid.SteamId, app uint32) (*Inventory, error) {
	// Application-specific inventories share the IEconItems API shape.
	response, err := c.Call(ctx, http.MethodGet, "IEconItems_"+strconv.FormatUint(uint64(app), 10), "GetPlayerItems", 1, url.Values{"steamid": {strconv.FormatUint(uint64(id), 10)}})
	if err != nil {
		return nil, err
	}

	// Preserve the inventory status so callers can distinguish visibility outcomes.
	result := &Inventory{}
	if err := Decode(response, result, "result"); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSchema retrieves the selected application's economy schema.
func (c *Client) GetSchema(ctx context.Context, app uint32, language string) (*Schema, error) {
	response, err := c.Call(ctx, http.MethodGet, "IEconItems_"+strconv.FormatUint(uint64(app), 10), "GetSchema", 1, url.Values{"language": {language}})
	if err != nil {
		return nil, err
	}
	result := &Schema{}
	if err := Decode(response, result, "result"); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAssetPrices returns the store's listed assets in the requested currency.
func (c *Client) GetAssetPrices(ctx context.Context, app uint32, language, currency string) ([]*Asset, error) {
	// Request the localized price list through the same bounded transport.
	response, err := c.Call(ctx, http.MethodGet, "ISteamEconomy", "GetAssetPrices", 1, url.Values{"appid": {strconv.FormatUint(uint64(app), 10)}, "language": {language}, "currency": {currency}})
	if err != nil {
		return nil, err
	}

	// A provider rejection is distinct from an empty store.
	if !response.GetBool("result", "success") {
		return nil, errors.New("Steam asset price lookup failed")
	}
	return decodeList[Asset, *Asset](response, "result", "assets")
}

// GetAssetClassInfo reads the requested class, independent of map iteration order.
func (c *Client) GetAssetClassInfo(ctx context.Context, app uint32, class uint64, language string) (*AssetClassInfo, error) {
	// Class IDs are decimal object keys in Valve's response envelope.
	classID := strconv.FormatUint(class, 10)
	response, err := c.Call(ctx, http.MethodGet, "ISteamEconomy", "GetAssetClassInfo", 1, url.Values{"appid": {strconv.FormatUint(uint64(app), 10)}, "language": {language}, "class_count": {"1"}, "classid0": {classID}})
	if err != nil {
		return nil, err
	}

	// Select the exact requested class instead of whichever map entry appears last.
	result := &AssetClassInfo{}
	if err := Decode(response, result, "result", classID); err != nil {
		return nil, err
	}
	result.ClassId = classID
	return result, nil
}

// Position returns the inventory slot encoded in an item's lower sixteen bits.
func (i *Item) Position() uint16 { return uint16(i.InventoryToken & 0xffff) }

// HasTag reports whether the store asset has the requested tag.
func (a *Asset) HasTag(tag string) bool { return slices.Contains(a.Tags, tag) }

// Item returns the schema's retained item with the requested definition index.
func (s *Schema) Item(index int32) *SchemaItem {
	for _, item := range s.Items {
		if item.Defindex == index {
			return item
		}
	}
	return nil
}
