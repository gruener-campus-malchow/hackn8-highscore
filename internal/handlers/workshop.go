package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/labstack/echo/v4"
)

type WorkshopHandler struct {
	DB *db.DB
}

func (h *WorkshopHandler) ShowRegister(c echo.Context) error {
	user := c.Get("user").(*models.User)
	activities, err := h.DB.GetUserActivities(user.ID)
	if err != nil {
		return err
	}
	allWorkshops, err := h.DB.GetAllWorkshopsWithCreators()
	if err != nil {
		return err
	}
	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "workshop-register.html", map[string]any{
		"User":         user,
		"Activities":   activities,
		"AllWorkshops": allWorkshops,
		"Config":       cfg,
		"Error":        c.QueryParam("error"),
	})
}

func (h *WorkshopHandler) Register(c echo.Context) error {
	user := c.Get("user").(*models.User)
	name := strings.TrimSpace(c.FormValue("name"))
	description := strings.TrimSpace(c.FormValue("description"))
	location := strings.TrimSpace(c.FormValue("location"))

	if name == "" {
		return c.Redirect(http.StatusFound, "/workshop/register?error=empty")
	}

	_, err := h.DB.CreateActivity(name, description, location, models.ActivityWorkshop, user.ID)
	if err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, "/workshop/register")
}

func (h *WorkshopHandler) ShowQR(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	isCollab, err := h.DB.IsCollaborator(id, user.ID)
	if err != nil {
		return err
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin && !isCollab {
		return echo.NewHTTPError(http.StatusForbidden)
	}
	isCreatorOrAdmin := activity.CreatedBy == user.ID || user.IsAdmin

	if !activity.Enabled && !user.IsAdmin {
		return c.Render(http.StatusOK, "workshop-qr.html", map[string]any{
			"User":             user,
			"Activity":         activity,
			"Pending":          true,
			"IsCreatorOrAdmin": isCreatorOrAdmin,
		})
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
		"User":             user,
		"Activity":         activity,
		"ScanURL":          scanURL,
		"QRBase64":         fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png)),
		"Pending":          false,
		"Config":           cfg,
		"LastScan":         lastScan,
		"IsCreatorOrAdmin": isCreatorOrAdmin,
	})
}

func (h *WorkshopHandler) ShowQRPartial(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	isCollab, err := h.DB.IsCollaborator(id, user.ID)
	if err != nil {
		return err
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin && !isCollab {
		return echo.NewHTTPError(http.StatusForbidden)
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

	return c.Render(http.StatusOK, "_workshop-qr-partial.html", map[string]any{
		"Activity": activity,
		"ScanURL":  scanURL,
		"QRBase64": fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png)),
		"Config":   cfg,
		"LastScan": lastScan,
	})
}

func (h *WorkshopHandler) RegenerateToken(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin {
		return echo.NewHTTPError(http.StatusForbidden)
	}
	if activity.Type != models.ActivityWorkshop {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if _, err := h.DB.RotateWorkshopToken(id); err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, fmt.Sprintf("/workshop/%d/qr", id))
}

// CollaboratorsPartial renders the collaborator section for a workshop (HTMX lazy load).
func (h *WorkshopHandler) CollaboratorsPartial(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin {
		return echo.NewHTTPError(http.StatusForbidden)
	}
	return h.renderCollaboratorsPartial(c, id, activity.CreatedBy)
}

func (h *WorkshopHandler) AddCollaborator(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin {
		return echo.NewHTTPError(http.StatusForbidden)
	}

	var collaboratorID int64
	if _, err := fmt.Sscanf(c.FormValue("user_id"), "%d", &collaboratorID); err != nil || collaboratorID == 0 || collaboratorID == activity.CreatedBy {
		return h.renderCollaboratorsPartial(c, id, activity.CreatedBy)
	}

	if err := h.DB.AddCollaborator(id, collaboratorID); err != nil && !isUniqueViolation(err) {
		return err
	}
	return h.renderCollaboratorsPartial(c, id, activity.CreatedBy)
}

func (h *WorkshopHandler) RemoveCollaborator(c echo.Context) error {
	user := c.Get("user").(*models.User)
	var id int64
	if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	activity, err := h.DB.GetActivityByID(id)
	if err != nil {
		return err
	}
	if activity == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	if activity.CreatedBy != user.ID && !user.IsAdmin {
		return echo.NewHTTPError(http.StatusForbidden)
	}

	var collaboratorID int64
	if _, err := fmt.Sscanf(c.FormValue("user_id"), "%d", &collaboratorID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if err := h.DB.RemoveCollaborator(id, collaboratorID); err != nil {
		return err
	}
	return h.renderCollaboratorsPartial(c, id, activity.CreatedBy)
}

func (h *WorkshopHandler) renderCollaboratorsPartial(c echo.Context, activityID, creatorID int64) error {
	allUsers, err := h.DB.GetAllUsers()
	if err != nil {
		return err
	}
	collabs, err := h.DB.GetCollaborators(activityID)
	if err != nil {
		return err
	}
	collabSet := make(map[int64]bool)
	for _, co := range collabs {
		collabSet[co.ID] = true
	}
	var avail []*models.User
	for _, u := range allUsers {
		if u.ID == creatorID || collabSet[u.ID] {
			continue
		}
		avail = append(avail, u)
	}
	return c.Render(http.StatusOK, "_collaborators-partial.html", map[string]any{
		"ActivityID":     activityID,
		"Collaborators":  collabs,
		"AvailableUsers": avail,
	})
}

func baseURL(c echo.Context) string {
	scheme := "http"
	if c.Request().TLS != nil || c.Request().Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request().Host)
}
