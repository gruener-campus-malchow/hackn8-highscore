package handlers

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/pretix"
	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"
)

// attendeeCount returns the Pretix checked-in count when use_pretix_checkin is
// enabled, falling back to the local registered user count.
func attendeeCount(cfg *models.Config, database *db.DB) (int, error) {
	if cfg.UsePretixCheckin {
		if server := os.Getenv("PRETIX_SERVER"); server != "" {
			n, err := pretix.GetCheckedInCount(server, os.Getenv("PRETIX_ORGANIZER"), os.Getenv("PRETIX_EVENT"), os.Getenv("PRETIX_API_KEY"))
			if err != nil {
				log.Printf("pretix checked-in count error (falling back to local): %v", err)
			} else {
				return n, nil
			}
		}
	}
	return database.GetUserCount()
}

type LeaderboardHandler struct {
	DB *db.DB
}

type leaderboardData struct {
	Entries      []*models.LeaderboardEntry
	TotalPoints  int
	Threshold    int
	GamingActive bool
	Progress     int // 0-100
	User         any
	DashboardQR  string // base64 data URI for the site URL QR code
	PublicURL    string
}

func (h *LeaderboardHandler) buildData(user any) (*leaderboardData, error) {
	cfg, err := h.DB.GetConfig()
	if err != nil {
		return nil, err
	}
	entries, err := h.DB.GetLeaderboard()
	if err != nil {
		return nil, err
	}
	total, err := h.DB.GetTotalPoints()
	if err != nil {
		return nil, err
	}
	userCount, err := attendeeCount(cfg, h.DB)
	if err != nil {
		return nil, err
	}

	if user != nil {
		u := user.(*models.User)
		for _, e := range entries {
			if e.UserID == u.ID {
				e.IsCurrentUser = true
			}
		}
	}

	threshold := db.ComputeEffectiveThreshold(cfg, userCount)
	progress := 0
	if threshold > 0 {
		progress = (total * 100) / threshold
		if progress > 100 {
			progress = 100
		}
	}

	return &leaderboardData{
		Entries:      entries,
		TotalPoints:  total,
		Threshold:    threshold,
		GamingActive: total >= threshold,
		Progress:     progress,
		User:         user,
	}, nil
}

func (h *LeaderboardHandler) ShowLeaderboard(c echo.Context) error {
	user := c.Get("user")
	data, err := h.buildData(user)
	if err != nil {
		return err
	}

	url := baseURL(c)
	data.PublicURL = url
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		log.Printf("dashboard QR encode error: %v", err)
	} else {
		data.DashboardQR = fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png))
	}

	return c.Render(http.StatusOK, "leaderboard.html", data)
}

func (h *LeaderboardHandler) LeaderboardPartial(c echo.Context) error {
	user := c.Get("user")
	data, err := h.buildData(user)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "_leaderboard-partial.html", data)
}

func (h *LeaderboardHandler) ScoreAPI(c echo.Context) error {
	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	total, err := h.DB.GetTotalPoints()
	if err != nil {
		return err
	}
	userCount, err := attendeeCount(cfg, h.DB)
	if err != nil {
		return err
	}
	threshold := db.ComputeEffectiveThreshold(cfg, userCount)

	eventName := os.Getenv("EVENT_NAME")
	if eventName == "" {
		eventName = "hackn8"
	}

	return c.JSON(http.StatusOK, map[string]any{
		eventName: map[string]int{
			"current_score":    total,
			"gaming_threshold": threshold,
		},
	})
}
