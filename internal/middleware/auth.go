package middleware

import (
	"net/http"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const SessionName = "hackn8"
const SessionUserID = "user_id"

func GetSession(c echo.Context, store sessions.Store) (*sessions.Session, error) {
	return store.Get(c.Request(), SessionName)
}

func GetCurrentUser(c echo.Context, store sessions.Store, database *db.DB) *models.User {
	sess, err := GetSession(c, store)
	if err != nil {
		return nil
	}
	val, ok := sess.Values[SessionUserID]
	if !ok {
		return nil
	}
	id, ok := val.(int64)
	if !ok {
		return nil
	}
	user, _ := database.GetUserByID(id)
	return user
}

func RequireLogin(store sessions.Store, database *db.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetCurrentUser(c, store, database)
			if user == nil {
				return c.Redirect(http.StatusFound, "/auth/login?next="+c.Request().URL.Path)
			}
			c.Set("user", user)
			return next(c)
		}
	}
}

func RequireAdmin(store sessions.Store, database *db.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetCurrentUser(c, store, database)
			if user == nil {
				return c.Redirect(http.StatusFound, "/auth/login?next="+c.Request().URL.Path)
			}
			if !user.IsAdmin {
				return echo.NewHTTPError(http.StatusForbidden, "Admin access required")
			}
			c.Set("user", user)
			return next(c)
		}
	}
}

func InjectUser(store sessions.Store, database *db.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetCurrentUser(c, store, database)
			if user != nil {
				c.Set("user", user)
			}
			return next(c)
		}
	}
}
