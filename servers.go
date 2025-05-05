package steam

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/paralin/go-steam/netutil"
)

/*
	We don't really need anything but the endpoint address.
	Response example:

	{
		"endpoint":"162.254.197.38:27017",
		"legacy_endpoint":"162.254.197.38:27017",
		"type":"netfilter",
		"dc":"fra1",
		"realm":"steamglobal",
		"load":30,
		"wtd_load":25.342738151550293
	}

*/

type CMListResponse struct {
	Serverlist ServerList `json:"response"`
}

type ServerList struct {
	EndpointList []EndpointObject `json:"serverlist"`
}

type EndpointObject struct {
	EndpointIP string `json:"endpoint"`
}

// Doesn't require an API key
const CMListFetchURL string = "http://api.steampowered.com/ISteamDirectory/GetCMListForConnect/v1/?cmtype=netfilter"

func FetchCMList() CMListResponse {
	var result CMListResponse
	apiResponse, err := http.Get(CMListFetchURL)
	if err != nil {
		log.Fatal("failed to fetch cm server list", err)
		return CMListResponse{}
	}

	apiResponseBody, err := io.ReadAll(apiResponse.Body)
	if err != nil {
		log.Fatal("failed to read the api response", err)
		return CMListResponse{}
	}

	if err = json.Unmarshal(apiResponseBody, &result); err != nil {
		log.Fatal("failed to unmarshal the api response", err)
		return CMListResponse{}
	}

	return result
}

// Gets the best CM server from the list according to it's latency
// Naming stays the same due to compatibility reasons.
func GetRandomCM() *netutil.PortAddr {
	cmServerList := FetchCMList()
	smallestLatencyAddr := ""
	var smallestLatency int64 = 1000 // random
	for i := 0; i < len(cmServerList.Serverlist.EndpointList); i++ {
		ipAddr := netutil.ParsePortAddr(cmServerList.Serverlist.EndpointList[i].EndpointIP)
		curTime := time.Now()
		conn, err := net.DialTCP("tcp", nil, ipAddr.ToTCPAddr())
		if err != nil {
			log.Fatal("failed to get a CM server", err)
			return nil
		}
		latency := time.Since(curTime).Milliseconds()
		conn.Close()
		if latency < smallestLatency {
			smallestLatency = latency
			smallestLatencyAddr = cmServerList.Serverlist.EndpointList[i].EndpointIP
		}
	}

	return netutil.ParsePortAddr(smallestLatencyAddr)
}
