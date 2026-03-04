package pretix

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var client = &http.Client{Timeout: 5 * time.Second}

var checkedInCache struct {
	sync.Mutex
	count     int
	fetchedAt time.Time
	cacheKey  string
}

const checkedInCacheTTL = 60 * time.Second

// GetCheckedInCount returns the number of order positions that have been
// checked in at least once. Results are cached for 60 seconds.
func GetCheckedInCount(server, organizer, event, apiKey string) (int, error) {
	key := server + "|" + organizer + "|" + event
	checkedInCache.Lock()
	defer checkedInCache.Unlock()
	if checkedInCache.cacheKey == key && time.Since(checkedInCache.fetchedAt) < checkedInCacheTTL {
		return checkedInCache.count, nil
	}

	url := fmt.Sprintf("%s/api/v1/organizers/%s/events/%s/orderpositions/?has_checkin=true&page_size=1", server, organizer, event)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("pretix API returned %d", resp.StatusCode)
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	checkedInCache.count = result.Count
	checkedInCache.fetchedAt = time.Now()
	checkedInCache.cacheKey = key
	return result.Count, nil
}

type OrderPosition struct {
	PositionID   int    `json:"positionid"`
	AttendeeName string `json:"attendee_name"`
}

type Order struct {
	Status    string          `json:"status"`
	Positions []OrderPosition `json:"positions"`
}

// GetOrder fetches an order from Pretix. Returns nil if not found.
func GetOrder(server, organizer, event, apiKey, code string) (*Order, error) {
	url := fmt.Sprintf("%s/api/v1/organizers/%s/events/%s/orders/%s/", server, organizer, event, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pretix API returned %d", resp.StatusCode)
	}

	var order Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return nil, err
	}
	return &order, nil
}
