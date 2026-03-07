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
	User       *models.User   // logged-in user
	Subject    *models.User   // user whose score is shown (= User when viewing own)
	Scans      []*db.UserScanEntry
	Rank       int
	IsAdmin    bool
	AllUsers   []*models.User
	ViewedUser *models.User // non-nil when admin views another user
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

	data := myScoreData{
		User:    user,
		Subject: user,
		Scans:   scans,
		Rank:    rank,
		IsAdmin: user.IsAdmin,
	}

	if user.IsAdmin {
		users, err := h.DB.GetAllUsers()
		if err != nil {
			return err
		}
		data.AllUsers = users
	}

	return c.Render(http.StatusOK, "myscore.html", data)
}
