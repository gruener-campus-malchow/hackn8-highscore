package handlers

import (
	"net/http"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/labstack/echo/v4"
)

type MyScoreHandler struct {
	DB *db.DB
}

type myScoreData struct {
	User  *models.User
	Scans []*db.UserScanEntry
	Rank  int
}

func (h *MyScoreHandler) ShowMyScore(c echo.Context) error {
	user := c.Get("user").(*models.User)

	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}

	scans, err := h.DB.GetUserScans(user.ID, cfg)
	if err != nil {
		return err
	}

	rank, err := h.DB.GetUserRank(user.ID)
	if err != nil {
		return err
	}

	return c.Render(http.StatusOK, "myscore.html", myScoreData{
		User:  user,
		Scans: scans,
		Rank:  rank,
	})
}
