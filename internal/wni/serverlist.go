package wni

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
)

// ServerListURL is the real WNI endpoint EEWSock::get_server_list fetches.
const ServerListURL = "http://lst10s-sp.wni.co.jp/server_list.txt"

// FetchServerList retrieves the newline-separated "host:port" relay server
// list, matching EEWSock::get_server_list.
func FetchServerList(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastCaster/1.0 powered by weathernews.")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wni: fetching server list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wni: fetching server list: status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wni: reading server list: %w", err)
	}

	var servers []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			servers = append(servers, line)
		}
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("wni: server list was empty")
	}
	return servers, nil
}

// ChooseServer picks a random entry, matching EEWSock::choose_server.
func ChooseServer(servers []string) string {
	return servers[rand.Intn(len(servers))]
}
