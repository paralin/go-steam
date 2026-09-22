package web_test

import (
	"io"
	"net/http"
	"testing"
)

// TestAppDirectoryPagination follows store cursors and retains incremental metadata.
func TestAppDirectoryPagination(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/IStoreService/GetAppList/v1/" || r.URL.Query().Get("include_dlc") != "true" {
			t.Error("unexpected directory request", r.URL.Path)
		}
		switch r.URL.Query().Get("last_appid") {
		case "0":
			io.WriteString(w, `{"response":{"apps":[{"appid":440,"name":"TF2","last_modified":123,"price_change_number":4}],"last_appid":440,"have_more_results":true}}`)
		case "440":
			io.WriteString(w, `{"response":{"apps":[{"appid":570,"name":"Dota 2"}],"last_appid":570}}`)
		default:
			t.Error("incorrect continuation cursor")
			w.WriteHeader(http.StatusBadRequest)
		}
	}, 0)

	apps, err := client.GetAppList(t.Context())
	if err != nil || len(apps) != 2 || apps[0].LastModified != 123 || apps[0].PriceChangeNumber != 4 || apps[1].AppId != 570 {
		t.Fatalf("directory: %v, %v", apps, err)
	}
}

// TestAppDirectoryStalledCursor returns the partial directory and a bounded failure.
func TestAppDirectoryStalledCursor(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"response":{"apps":[{"appid":440}],"have_more_results":true}}`)
	}, 0)

	apps, err := client.GetAppList(t.Context())
	if err == nil || len(apps) != 1 {
		t.Fatalf("stalled cursor: %v, %v", apps, err)
	}
}
