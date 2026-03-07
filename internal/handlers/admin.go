package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"
)

type AdminHandler struct {
	DB *db.DB
}

func (h *AdminHandler) ShowDashboard(c echo.Context) error {
	user := c.Get("user").(*models.User)

	activities, err := h.DB.GetAllActivitiesWithCreators()
	if err != nil {
		return err
	}
	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	userCount, err := h.DB.GetUserCount()
	if err != nil {
		return err
	}
	scanCount, err := h.DB.GetScanCount()
	if err != nil {
		return err
	}
	totalPoints, err := h.DB.GetTotalPoints()
	if err != nil {
		return err
	}
	users, err := h.DB.GetAllUsers()
	if err != nil {
		return err
	}
	attendees, err := attendeeCount(cfg, h.DB)
	if err != nil {
		return err
	}

	return c.Render(http.StatusOK, "admin.html", map[string]any{
		"User":               user,
		"Activities":         activities,
		"Users":              users,
		"Config":             cfg,
		"UserCount":          userCount,
		"AttendeeCount":      attendees,
		"ScanCount":          scanCount,
		"TotalPoints":        totalPoints,
		"EffectiveThreshold": db.ComputeEffectiveThreshold(cfg, attendees),
		"Error":              c.QueryParam("error"),
		"Success":            c.QueryParam("success"),
	})
}

func (h *AdminHandler) PromoteUser(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if err := h.DB.SetAdminStatus(id, true); err != nil {
		return err
	}
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		return c.JSON(http.StatusOK, map[string]any{"is_admin": true})
	}
	return c.Redirect(http.StatusFound, "/admin?success=user_promoted")
}

func (h *AdminHandler) DemoteUser(c echo.Context) error {
	self := c.Get("user").(*models.User)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if id == self.ID {
		if c.Request().Header.Get("X-Requested-With") == "fetch" {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "cannot_demote_self"})
		}
		return c.Redirect(http.StatusFound, "/admin?error=cannot_demote_self")
	}
	if err := h.DB.SetAdminStatus(id, false); err != nil {
		return err
	}
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		return c.JSON(http.StatusOK, map[string]any{"is_admin": false})
	}
	return c.Redirect(http.StatusFound, "/admin?success=user_demoted")
}

func (h *AdminHandler) ToggleActivity(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	enabled, err := h.DB.ToggleActivity(id)
	if err != nil {
		return err
	}
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		return c.JSON(http.StatusOK, map[string]any{"enabled": enabled})
	}
	return c.Redirect(http.StatusFound, "/admin")
}

func (h *AdminHandler) SetActivityPoints(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	raw := strings.TrimSpace(c.FormValue("points"))
	if raw == "" {
		if err := h.DB.SetActivityPoints(id, nil); err != nil {
			return err
		}
	} else {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return c.Redirect(http.StatusFound, "/admin?error=invalid_points")
		}
		if err := h.DB.SetActivityPoints(id, &n); err != nil {
			return err
		}
	}
	return c.Redirect(http.StatusFound, "/admin")
}

func (h *AdminHandler) CreateHiddenActivity(c echo.Context) error {
	user := c.Get("user").(*models.User)
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Redirect(http.StatusFound, "/admin?error=empty_name")
	}

	var pointsPtr *int
	raw := strings.TrimSpace(c.FormValue("points"))
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return c.Redirect(http.StatusFound, "/admin?error=invalid_points")
		}
		pointsPtr = &n
	}

	activity, err := h.DB.CreateHiddenActivity(name, pointsPtr, user.ID)
	if err != nil {
		return err
	}

	if msg := strings.TrimSpace(c.FormValue("scan_message")); msg != "" {
		if err := h.DB.SetActivityScanMessage(activity.ID, msg); err != nil {
			return err
		}
	}

	return c.Redirect(http.StatusFound, "/admin?success=hidden_created")
}

func (h *AdminHandler) SetActivityScanMessage(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	msg := strings.TrimSpace(c.FormValue("scan_message"))
	if err := h.DB.SetActivityScanMessage(id, msg); err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, "/admin")
}

func (h *AdminHandler) UpdateConfig(c echo.Context) error {
	if mode := strings.TrimSpace(c.FormValue("gaming_threshold_mode")); mode == "absolute" || mode == "multiplier" {
		if err := h.DB.SetConfig("gaming_threshold_mode", mode); err != nil {
			return err
		}
	}
	fields := map[string]string{
		"gaming_threshold":            c.FormValue("gaming_threshold"),
		"gaming_threshold_multiplier": c.FormValue("gaming_threshold_multiplier"),
		"default_workshop_points":     c.FormValue("default_workshop_points"),
		"default_hidden_points":       c.FormValue("default_hidden_points"),
		"penalty_points":              c.FormValue("penalty_points"),
		"creator_bonus_percentage":    c.FormValue("creator_bonus_percentage"),
	}
	for key, val := range fields {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return c.Redirect(http.StatusFound, "/admin?error=invalid_config")
		}
		if err := h.DB.SetConfig(key, strconv.Itoa(n)); err != nil {
			return err
		}
	}
	// Checkboxes: not submitted when unchecked, so always write the resolved value
	usePretix := "0"
	if c.FormValue("use_pretix_checkin") == "1" {
		usePretix = "1"
	}
	if err := h.DB.SetConfig("use_pretix_checkin", usePretix); err != nil {
		return err
	}
	requirePretix := "0"
	if c.FormValue("require_pretix_login") == "1" {
		requirePretix = "1"
	}
	if err := h.DB.SetConfig("require_pretix_login", requirePretix); err != nil {
		return err
	}
	if re := strings.TrimSpace(c.FormValue("ticket_code_regex")); re != "" {
		if _, err := regexp.Compile(re); err != nil {
			return c.Redirect(http.StatusFound, "/admin?error=invalid_config")
		}
		if err := h.DB.SetConfig("ticket_code_regex", re); err != nil {
			return err
		}
	}
	return c.Redirect(http.StatusFound, "/admin?success=config_saved")
}

func (h *AdminHandler) ShowQR(c echo.Context) error {
	user := c.Get("user").(*models.User)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	lastScan, err := h.DB.GetLastScan(activity.ID)
	if err != nil {
		return err
	}

	scanURL := fmt.Sprintf("%s/scan/%s", baseURL(c), activity.Token)
	png, err := qrcode.Encode(scanURL, qrcode.Medium, 256)
	if err != nil {
		return err
	}

	return c.Render(http.StatusOK, "workshop-qr.html", map[string]any{
		"User":     user,
		"Activity": activity,
		"ScanURL":  scanURL,
		"QRBase64": fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png)),
		"Pending":  false,
		"IsAdmin":  true,
		"Config":   cfg,
		"LastScan": lastScan,
	})
}

func (h *AdminHandler) ToggleLeaderboardHidden(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	user, err := h.DB.GetUserByID(id)
	if err != nil || user == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	if err := h.DB.SetLeaderboardHidden(id, !user.HiddenFromLeaderboard); err != nil {
		return err
	}
	newHidden := !user.HiddenFromLeaderboard
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		return c.JSON(http.StatusOK, map[string]any{"hidden": newHidden})
	}
	if user.HiddenFromLeaderboard {
		return c.Redirect(http.StatusFound, "/admin?success=leaderboard_shown")
	}
	return c.Redirect(http.StatusFound, "/admin?success=leaderboard_hidden")
}

func (h *AdminHandler) AdjustUserPoints(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	raw := strings.TrimSpace(c.FormValue("delta"))
	if raw == "" {
		return c.Redirect(http.StatusFound, "/admin?error=invalid_points")
	}
	delta, err := strconv.Atoi(raw)
	if err != nil {
		return c.Redirect(http.StatusFound, "/admin?error=invalid_points")
	}
	if err := h.DB.AddPoints(id, delta); err != nil {
		return err
	}
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		u, err := h.DB.GetUserByID(id)
		if err != nil || u == nil {
			return c.JSON(http.StatusOK, map[string]any{})
		}
		return c.JSON(http.StatusOK, map[string]any{"total_points": u.TotalPoints})
	}
	return c.Redirect(http.StatusFound, "/admin?success=points_adjusted")
}

func (h *AdminHandler) ShowUserScore(c echo.Context) error {
	admin := c.Get("user").(*models.User)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	target, err := h.DB.GetUserByID(id)
	if err != nil || target == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	scans, err := h.DB.GetUserScans(id, cfg)
	if err != nil {
		return err
	}
	rank, err := h.DB.GetUserRank(id)
	if err != nil {
		return err
	}
	users, err := h.DB.GetAllUsers()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "myscore.html", myScoreData{
		User:       admin,
		Subject:    target,
		Scans:      scans,
		Rank:       rank,
		IsAdmin:    true,
		AllUsers:   users,
		ViewedUser: target,
	})
}

func (h *AdminHandler) DeleteActivity(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if err := h.DB.DeleteActivity(id); err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, "/admin")
}

func (h *AdminHandler) ToggleCreatorBonus(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	bonus, err := h.DB.ToggleCreatorBonus(id)
	if err != nil {
		return err
	}
	if c.Request().Header.Get("X-Requested-With") == "fetch" {
		return c.JSON(http.StatusOK, map[string]any{"creator_bonus": bonus})
	}
	return c.Redirect(http.StatusFound, "/admin")
}
