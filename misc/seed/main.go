// Seed script: populates hackn8.db with test data for development.
// Run from repo root: go run ./misc/seed/
package main

import (
	"fmt"
	"log"
	"math/rand"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/db"
	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
)

func ptr(n int) *int { return &n }

func main() {
	database, err := db.New("hackn8.db")
	if err != nil {
		log.Fatal(err)
	}

	rng := rand.New(rand.NewSource(42))

	// --- Users ---
	names := []string{
		"Alice", "Bob", "Charlie", "Diana", "Erik",
		"Fiona", "Georg", "Hanna", "Ivan", "Julia",
		"Karl", "Laura", "Max", "Nina", "Otto",
		"Paula", "Quentin", "Rosa", "Stefan", "Tina",
		"Ulf", "Vera", "Willy", "Xena", "Yuki",
		"Zara", "Arne", "Britta", "Carsten", "Dani",
		"Elias", "Frida", "Gunnar", "Helga", "Ingo",
		"Jana", "Klaus", "Lisa", "Moritz", "Nadja",
	}

	users := make([]*models.User, 0, len(names))
	for i, name := range names {
		code := fmt.Sprintf("U%04d", i+1)
		user, _, err := database.GetOrCreateUser(code)
		if err != nil {
			log.Fatalf("create user %s: %v", code, err)
		}
		if err := database.SetNickname(user.ID, name); err != nil {
			log.Fatalf("set nickname %s: %v", name, err)
		}
		users = append(users, user)
	}
	if err := database.SetAdmin(users[0].ID); err != nil {
		log.Fatalf("set admin: %v", err)
	}
	fmt.Printf("Seeded %d users (admin: %s).\n", len(users), names[0])

	cfg, err := database.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	// --- Workshops ---
	type workshopDef struct {
		name     string
		desc     string
		location string
		enabled  bool
		points   *int
	}
	workshops := []workshopDef{
		{"Intro to Linux", "Grundlagen der Linux-Shell für Einsteiger", "Seminarraum A", true, nil},
		{"CTF Workshop", "Capture-the-Flag: Web-Exploitation & Forensics", "Hackerspace (Keller)", true, ptr(150)},
		{"Netzwerk-Grundlagen", "TCP/IP, Subnetting, Wireshark-Demo", "Seminarraum B", true, nil},
		{"Hardware Hacking", "Lötstation, UART, JTAG und mehr", "Werkstatt", true, ptr(200)},
		{"Coding mit Python", "Skripte, Automationen & kleine Spiele", "Seminarraum A", false, nil},
		{"Machine Learning Basics", "KNN, Trainingsdaten, Demo mit MNIST", "Seminarraum B", false, nil},
	}

	type actEntry struct {
		id     int64
		points int
	}
	var workshopActs []actEntry

	for i, w := range workshops {
		creator := users[i%5] // first 5 users are workshop creators
		act, err := database.CreateActivity(w.name, w.desc, w.location, models.ActivityWorkshop, creator.ID)
		if err != nil {
			log.Fatalf("create workshop %q: %v", w.name, err)
		}
		if w.points != nil {
			if err := database.SetActivityPoints(act.ID, w.points); err != nil {
				log.Fatalf("set points %q: %v", w.name, err)
			}
			act.Points = w.points
		}
		if w.enabled {
			if _, err := database.ToggleActivity(act.ID); err != nil {
				log.Fatalf("enable workshop %q: %v", w.name, err)
			}
		}
		pts := cfg.DefaultWorkshopPoints
		if act.Points != nil {
			pts = *act.Points
		}
		workshopActs = append(workshopActs, actEntry{act.ID, pts})
		status := "disabled"
		if w.enabled {
			status = "enabled "
		}
		fmt.Printf("  Workshop [%s] %-35q %3d pts\n", status, w.name, pts)
	}

	// --- Hidden QR codes ---
	type hiddenDef struct {
		name   string
		points *int
	}
	hiddens := []hiddenDef{
		{"Serverraum gefunden!", nil},
		{"Unter dem Tisch", nil},
		{"Easter Egg im Klo", nil},
		{"Versteckt am Eingang", ptr(50)},
		{"Dachboden-Geheimnis", ptr(75)},
	}

	var hiddenActs []actEntry
	admin := users[0]
	for _, h := range hiddens {
		act, err := database.CreateHiddenActivity(h.name, h.points, admin.ID)
		if err != nil {
			log.Fatalf("create hidden %q: %v", h.name, err)
		}
		pts := cfg.DefaultHiddenPoints
		if act.Points != nil {
			pts = *act.Points
		}
		hiddenActs = append(hiddenActs, actEntry{act.ID, pts})
		fmt.Printf("  Hidden   [enabled ] %-35q %3d pts\n", h.name, pts)
	}

	// --- Scans ---
	// Enabled workshops: ~75% of users attend each one
	// Hidden codes: ~35% of users find each one
	scanCount := 0

	for _, act := range workshopActs {
		for _, u := range users {
			if rng.Float64() < 0.75 {
				if err := database.RecordScan(u.ID, act.id); err != nil {
					continue // already scanned (idempotent re-run)
				}
				if err := database.AddPoints(u.ID, act.points); err != nil {
					log.Fatalf("add points: %v", err)
				}
				scanCount++
			}
		}
	}

	for _, act := range hiddenActs {
		for _, u := range users {
			if rng.Float64() < 0.35 {
				if err := database.RecordScan(u.ID, act.id); err != nil {
					continue
				}
				if err := database.AddPoints(u.ID, act.points); err != nil {
					log.Fatalf("add points: %v", err)
				}
				scanCount++
			}
		}
	}

	fmt.Printf("Seeded %d scans.\n", scanCount)
}
