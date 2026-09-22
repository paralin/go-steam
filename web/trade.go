package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
)

// TradeFilter selects one page of sent or received offers.
type TradeFilter struct {
	// Sent includes offers created by this account.
	Sent bool
	// Received includes offers addressed to this account.
	Received bool
	// Descriptions includes localized item descriptions.
	Descriptions bool
	// Language selects Valve's item-description language.
	Language string
	// ActiveOnly restricts results to active or recently changed offers.
	ActiveOnly bool
	// HistoricalOnly restricts results to completed offers.
	HistoricalOnly bool
	// HistoricalCutoff is the inclusive update cutoff in Unix seconds.
	HistoricalCutoff uint32
	// Cursor continues a previous response when Valve supplies a next cursor.
	Cursor uint64
}

// GetTradeOffers returns one page, including descriptions and a continuation cursor.
func (c *Client) GetTradeOffers(ctx context.Context, filter TradeFilter) (*TradeOffers, error) {
	// Keep pagination explicit so callers control history depth and request cost.
	values := url.Values{
		"get_sent_offers":        {strconv.FormatBool(filter.Sent)},
		"get_received_offers":    {strconv.FormatBool(filter.Received)},
		"get_descriptions":       {strconv.FormatBool(filter.Descriptions)},
		"language":               {filter.Language},
		"active_only":            {strconv.FormatBool(filter.ActiveOnly)},
		"historical_only":        {strconv.FormatBool(filter.HistoricalOnly)},
		"time_historical_cutoff": {strconv.FormatUint(uint64(filter.HistoricalCutoff), 10)},
		"cursor":                 {strconv.FormatUint(filter.Cursor, 10)},
	}
	response, err := c.Call(ctx, http.MethodGet, "IEconService", "GetTradeOffers", 1, values)
	if err != nil {
		return nil, err
	}

	// Retain display metadata beside the typed offers and resolve their item names.
	result := &TradeOffers{}
	if err := Decode(response, result, "response"); err != nil {
		return nil, err
	}
	populateDescriptions(result.Sent, result.Descriptions)
	populateDescriptions(result.Received, result.Descriptions)
	return result, nil
}

// GetTradeOffer returns one offer with its localized item names attached.
func (c *Client) GetTradeOffer(ctx context.Context, id uint64, language string) (*TradeOffer, error) {
	// Fetch the offer and its descriptions in one request.
	response, err := c.Call(ctx, http.MethodGet, "IEconService", "GetTradeOffer", 1, url.Values{"tradeofferid": {strconv.FormatUint(id, 10)}, "language": {language}})
	if err != nil {
		return nil, err
	}
	result := &TradeOffer{}
	if err := Decode(response, result, "response", "offer"); err != nil {
		return nil, err
	}
	if result.State == 0 {
		return nil, errors.New("Steam returned an invalid trade offer")
	}

	// Descriptions are optional when Valve omits display data.
	if response.Get("response", "descriptions") != nil {
		descriptions, err := decodeList[ItemDescription, *ItemDescription](response, "response", "descriptions")
		if err != nil {
			return nil, err
		}
		populateDescriptions([]*TradeOffer{result}, descriptions)
	}
	return result, nil
}

// CancelTradeOffer withdraws an offer sent by this account using one POST.
func (c *Client) CancelTradeOffer(ctx context.Context, id uint64) error {
	return c.tradeAction(ctx, "CancelTradeOffer", id)
}

// DeclineTradeOffer declines an incoming offer using one POST.
func (c *Client) DeclineTradeOffer(ctx context.Context, id uint64) error {
	return c.tradeAction(ctx, "DeclineTradeOffer", id)
}

// tradeAction sends an explicit mutation without retrying an uncertain outcome.
func (c *Client) tradeAction(ctx context.Context, method string, id uint64) error {
	response, err := c.Call(ctx, http.MethodPost, "IEconService", method, 1, url.Values{"tradeofferid": {strconv.FormatUint(id, 10)}})
	if err != nil {
		return err
	}
	if response.Get("error") != nil || response.Get("response", "error") != nil {
		return errors.New("Steam rejected the trade offer action")
	}
	if response.Get("response") == nil {
		return errors.New("Steam returned no trade offer action result")
	}
	if success := response.Get("response", "success"); success != nil && !success.GetBool() {
		return errors.New("Steam rejected the trade offer action")
	}

	return nil
}

// populateDescriptions matches exact app, class, and instance identities.
func populateDescriptions(offers []*TradeOffer, descriptions []*ItemDescription) {
	// Index once; similarly named classes in different applications stay distinct.
	type identity struct {
		// app separates each game's economy namespace.
		app uint32
		// class identifies the item's base definition.
		class uint64
		// instance distinguishes variants within a class.
		instance uint64
	}
	names := make(map[identity]string, len(descriptions))
	for _, item := range descriptions {
		names[identity{item.AppId, item.ClassId, item.InstanceId}] = item.MarketHashName
	}

	// Populate both sides without changing the response's item identities.
	for _, offer := range offers {
		for _, items := range [][]*TradeAsset{offer.ToGive, offer.ToReceive} {
			for _, item := range items {
				item.MarketHashName = names[identity{item.AppId, item.ClassId, item.InstanceId}]
			}
		}
	}
}
