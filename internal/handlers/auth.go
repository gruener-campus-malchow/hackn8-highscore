package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	appmw "github.com/gruener-campus-malchow/hackn8-highscore/internal/middleware"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/pretix"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// defaultTicketCodeRe is the fallback used by select-attendee routes (always Pretix 5-char codes).
var defaultTicketCodeRe = regexp.MustCompile(`^[A-Z0-9]{5}$`)

type AuthHandler struct {
	DB    *db.DB
	Store sessions.Store
}

func (h *AuthHandler) ShowLogin(c echo.Context) error {
	next := c.QueryParam("next")
	cfg, _ := h.DB.GetConfig()
	requirePretix := cfg != nil && cfg.RequirePretixLogin
	return c.Render(http.StatusOK, "login.html", map[string]any{
		"Next":               next,
		"Error":              c.QueryParam("error"),
		"RequirePretixLogin": requirePretix,
	})
}

func (h *AuthHandler) Login(c echo.Context) error {
	ticketCode := strings.ToUpper(strings.TrimSpace(c.FormValue("ticket_code")))
	next := c.FormValue("next")
	if next == "" {
		next = "/"
	}

	if ticketCode == "" {
		return c.Redirect(http.StatusFound, "/auth/login?error=empty")
	}

	cfg, err := h.DB.GetConfig()
	if err != nil {
		return err
	}
	codeRe, err := regexp.Compile(cfg.TicketCodeRegex)
	if err != nil {
		codeRe = defaultTicketCodeRe
	}
	if !codeRe.MatchString(ticketCode) {
		return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
	}

	if cfg.RequirePretixLogin && os.Getenv("DEBUG_NO_PRETIX") == "" {
		if server := os.Getenv("PRETIX_SERVER"); server != "" {
			order, err := pretix.GetOrder(server, os.Getenv("PRETIX_ORGANIZER"), os.Getenv("PRETIX_EVENT"), os.Getenv("PRETIX_API_KEY"), ticketCode)
			if err != nil {
				c.Logger().Errorf("pretix validation error: %v", err)
				return c.Redirect(http.StatusFound, "/auth/login?error=pretix_unavailable")
			}
			if order == nil || order.Status != "p" {
				return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
			}
			if len(order.Positions) > 1 {
				return c.Redirect(http.StatusFound, fmt.Sprintf("/auth/select-attendee?code=%s&next=%s", ticketCode, url.QueryEscape(next)))
			}
			if len(order.Positions) == 1 {
				ticketCode = fmt.Sprintf("%s-%d", ticketCode, order.Positions[0].PositionID)
			}
		}
	}

	return h.loginWithCode(c, ticketCode, next)
}

func (h *AuthHandler) ShowSelectAttendee(c echo.Context) error {
	code := strings.ToUpper(strings.TrimSpace(c.QueryParam("code")))
	next := c.QueryParam("next")
	if next == "" {
		next = "/"
	}
	if !defaultTicketCodeRe.MatchString(code) {
		return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
	}

	order, err := pretix.GetOrder(os.Getenv("PRETIX_SERVER"), os.Getenv("PRETIX_ORGANIZER"), os.Getenv("PRETIX_EVENT"), os.Getenv("PRETIX_API_KEY"), code)
	if err != nil {
		c.Logger().Errorf("pretix error: %v", err)
		return c.Redirect(http.StatusFound, "/auth/login?error=pretix_unavailable")
	}
	if order == nil || order.Status != "p" {
		return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
	}

	return c.Render(http.StatusOK, "select-attendee.html", map[string]any{
		"Code":      code,
		"Next":      next,
		"Positions": order.Positions,
	})
}

func (h *AuthHandler) SelectAttendee(c echo.Context) error {
	code := strings.ToUpper(strings.TrimSpace(c.FormValue("code")))
	next := c.FormValue("next")
	if next == "" {
		next = "/"
	}
	if !defaultTicketCodeRe.MatchString(code) {
		return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
	}

	positionID, err := strconv.Atoi(c.FormValue("position_id"))
	if err != nil || positionID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	order, err := pretix.GetOrder(os.Getenv("PRETIX_SERVER"), os.Getenv("PRETIX_ORGANIZER"), os.Getenv("PRETIX_EVENT"), os.Getenv("PRETIX_API_KEY"), code)
	if err != nil {
		c.Logger().Errorf("pretix error: %v", err)
		return c.Redirect(http.StatusFound, "/auth/login?error=pretix_unavailable")
	}
	if order == nil || order.Status != "p" {
		return c.Redirect(http.StatusFound, "/auth/login?error=invalid")
	}

	valid := false
	for _, p := range order.Positions {
		if p.PositionID == positionID {
			valid = true
			break
		}
	}
	if !valid {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	return h.loginWithCode(c, fmt.Sprintf("%s-%d", code, positionID), next)
}

func (h *AuthHandler) loginWithCode(c echo.Context, ticketCode, next string) error {
	user, isNew, err := h.DB.GetOrCreateUser(ticketCode)
	if err != nil {
		return err
	}

	adminTicket := strings.TrimSpace(os.Getenv("ADMIN_TICKET"))
	baseCode := strings.SplitN(ticketCode, "-", 2)[0]
	if isNew && adminTicket != "" && (ticketCode == adminTicket || baseCode == adminTicket) {
		_ = h.DB.SetAdmin(user.ID)
		_ = h.DB.SetLeaderboardHidden(user.ID, true)
		user.IsAdmin = true
		user.HiddenFromLeaderboard = true
	}
	if isNew && os.Getenv("DEBUG_ADMIN_ALL") != "" {
		_ = h.DB.SetAdmin(user.ID)
		user.IsAdmin = true
	}

	sess, err := appmw.GetSession(c, h.Store)
	if err != nil {
		return err
	}
	sess.Values[appmw.SessionUserID] = user.ID
	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return err
	}

	if isNew || user.Nickname == "" {
		return c.Redirect(http.StatusFound, "/auth/nickname?next="+url.QueryEscape(next))
	}
	return c.Redirect(http.StatusFound, next)
}

func (h *AuthHandler) Logout(c echo.Context) error {
	sess, err := appmw.GetSession(c, h.Store)
	if err != nil {
		return c.Redirect(http.StatusFound, "/")
	}
	sess.Options.MaxAge = -1
	_ = sess.Save(c.Request(), c.Response())
	return c.Redirect(http.StatusFound, "/")
}

func (h *AuthHandler) ShowNickname(c echo.Context) error {
	next := c.QueryParam("next")
	if next == "" {
		next = "/"
	}
	return c.Render(http.StatusOK, "nickname.html", map[string]any{
		"User":  c.Get("user"),
		"Next":  next,
		"Error": c.QueryParam("error"),
	})
}

func (h *AuthHandler) SetNickname(c echo.Context) error {
	user := c.Get("user").(*models.User)
	nickname := strings.TrimSpace(c.FormValue("nickname"))
	next := c.FormValue("next")
	if next == "" {
		next = "/"
	}

	if nickname == "" {
		return c.Redirect(http.StatusFound, "/auth/nickname?error=empty&next="+url.QueryEscape(next))
	}
	if utf8.RuneCountInString(nickname) > 32 {
		return c.Redirect(http.StatusFound, "/auth/nickname?error=toolong&next="+url.QueryEscape(next))
	}

	if err := h.DB.SetNickname(user.ID, nickname); err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, next)
}
