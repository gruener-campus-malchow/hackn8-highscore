package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/labstack/echo/v4"
)

type ScanHandler struct {
	DB *db.DB
}

func (h *ScanHandler) Scan(c echo.Context) error {
	user := c.Get("user").(*models.User)
	token := c.Param("token")

	activity, err := h.DB.GetActivityByToken(token)
	if err != nil {
		return err
	}
	if activity == nil {
		// Check if it's an invalidated workshop token (shared QR code cheat attempt)
		stale, err := h.DB.GetActivityByInvalidatedToken(token)
		if err != nil {
			return err
		}
		if stale != nil && stale.Type == models.ActivityWorkshop {
			already, err := h.DB.HasScanned(user.ID, stale.ID)
			if err != nil {
				return err
			}
			if already {
				return c.Render(http.StatusOK, "scan-result.html", map[string]any{
					"User":     user,
					"Status":   "already",
					"Activity": stale,
				})
			}
			cfg, err := h.DB.GetConfig()
			if err != nil {
				return err
			}
			actual, err := h.DB.ApplyPenalty(user.ID, cfg.PenaltyPoints)
			if err != nil {
				return err
			}
			return c.Render(http.StatusOK, "scan-result.html", map[string]any{
				"User":     user,
				"Status":   "penalty",
				"Activity": stale,
				"Penalty":  actual,
			})
		}
		return c.Render(http.StatusNotFound, "scan-result.html", map[string]any{
			"User":   user,
			"Status": "notfound",
		})
	}

	if !activity.Enabled {
		return c.Render(http.StatusOK, "scan-result.html", map[string]any{
			"User":     user,
			"Status":   "disabled",
			"Message":  "This activity is not yet enabled.",
			"Activity": activity,
		})
	}

	already, err := h.DB.HasScanned(user.ID, activity.ID)
	if err != nil {
		return err
	}
	if already {
		return c.Render(http.StatusOK, "scan-result.html", map[string]any{
			"User":     user,
			"Status":   "already",
			"Message":  "You've already scanned this code.",
			"Activity": activity,
		})
	}

	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	points := h.DB.ResolvePoints(activity, cfg)

	if err := h.DB.RecordScan(user.ID, activity.ID); err != nil {
		// Handle race condition (unique constraint)
		if isUniqueViolation(err) {
			return c.Render(http.StatusOK, "scan-result.html", map[string]any{
				"User":     user,
				"Status":   "already",
				"Message":  "You've already scanned this code.",
				"Activity": activity,
			})
		}
		return err
	}
	if err := h.DB.AddPoints(user.ID, points); err != nil {
		return err
	}

	// Rotate token for workshop activities so shared QR codes become stale
	if activity.Type == models.ActivityWorkshop {
		if _, err := h.DB.RotateWorkshopToken(activity.ID); err != nil {
			c.Logger().Errorf("token rotation failed for activity %d: %v", activity.ID, err)
		}
	}

	return c.Render(http.StatusOK, "scan-result.html", map[string]any{
		"User":     user,
		"Status":   "success",
		"Points":   points,
		"Activity": activity,
	})
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc/sqlite returns error with "UNIQUE constraint failed"
	return err != sql.ErrNoRows && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
