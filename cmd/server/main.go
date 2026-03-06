package main

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/handlers"
	appmw "github.com/gruener-campus-malchow/hackn8-highscore/internal/middleware"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var funcMap = template.FuncMap{
	"add":     func(a, b int) int { return a + b },
	"safeURL": func(s string) template.URL { return template.URL(s) },
}

// TemplateRenderer parses base.html + page template on each render to avoid
// name conflicts when multiple pages define "content"/"title" blocks.
type TemplateRenderer struct{}

func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	if strings.HasPrefix(name, "_") {
		// Partial template (no base layout)
		tmpl, err := template.New("").Funcs(funcMap).ParseFiles("templates/" + name)
		if err != nil {
			return err
		}
		return tmpl.ExecuteTemplate(w, "partial", data)
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(
		"templates/base.html",
		"templates/"+name,
	)
	if err != nil {
		return err
	}
	return tmpl.ExecuteTemplate(w, "base", data)
}

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("WARNING: could not load .env: %v", err)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "hackn8.db"
	}
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "change-me-in-production-please"
		log.Println("WARNING: using default session secret")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	e := echo.New()
	e.Renderer = &TemplateRenderer{}
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(appmw.InjectUser(store, database))
	e.Static("/static", "static")

	authH := &handlers.AuthHandler{DB: database, Store: store}
	lbH := &handlers.LeaderboardHandler{DB: database}
	scanH := &handlers.ScanHandler{DB: database}
	workshopH := &handlers.WorkshopHandler{DB: database}
	adminH := &handlers.AdminHandler{DB: database}
	myScoreH := &handlers.MyScoreHandler{DB: database}

	requireLogin := appmw.RequireLogin(store, database)
	requireAdmin := appmw.RequireAdmin(store, database)

	// Public
	e.GET("/", lbH.ShowLeaderboard)
	e.GET("/api/leaderboard", lbH.LeaderboardPartial)
	e.GET("/api/score", lbH.ScoreAPI)
	e.GET("/auth/login", authH.ShowLogin)
	e.POST("/auth/login", authH.Login)
	e.GET("/auth/select-attendee", authH.ShowSelectAttendee)
	e.POST("/auth/select-attendee", authH.SelectAttendee)
	e.POST("/auth/logout", authH.Logout)

	// Authenticated
	e.GET("/auth/nickname", authH.ShowNickname, requireLogin)
	e.POST("/auth/nickname", authH.SetNickname, requireLogin)
	e.GET("/myscore", myScoreH.ShowMyScore, requireLogin)
	e.GET("/scan/:token", scanH.Scan, requireLogin)
	e.GET("/workshop/register", workshopH.ShowRegister, requireLogin)
	e.POST("/workshop/register", workshopH.Register, requireLogin)
	e.GET("/workshop/:id/qr", workshopH.ShowQR, requireLogin)
	e.GET("/workshop/:id/qr-partial", workshopH.ShowQRPartial, requireLogin)
	e.POST("/workshop/:id/regenerate", workshopH.RegenerateToken, requireLogin)

	// Admin
	e.GET("/admin", adminH.ShowDashboard, requireAdmin)
	e.POST("/admin/activity/:id/toggle", adminH.ToggleActivity, requireAdmin)
	e.POST("/admin/activity/:id/points", adminH.SetActivityPoints, requireAdmin)
	e.POST("/admin/activity/hidden", adminH.CreateHiddenActivity, requireAdmin)
	e.POST("/admin/activity/:id/delete", adminH.DeleteActivity, requireAdmin)
	e.POST("/admin/activity/:id/toggle-creator-bonus", adminH.ToggleCreatorBonus, requireAdmin)
	e.GET("/admin/activity/:id/qr", adminH.ShowQR, requireAdmin)
	e.POST("/admin/config", adminH.UpdateConfig, requireAdmin)
	e.POST("/admin/user/:id/promote", adminH.PromoteUser, requireAdmin)
	e.POST("/admin/user/:id/demote", adminH.DemoteUser, requireAdmin)
	e.POST("/admin/user/:id/toggle-hidden", adminH.ToggleLeaderboardHidden, requireAdmin)
	e.POST("/admin/user/:id/points", adminH.AdjustUserPoints, requireAdmin)

	log.Printf("Starting on %s", addr)
	e.Logger.Fatal(e.Start(addr))
}
