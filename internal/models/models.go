package models

import "time"

type User struct {
	ID                   int64
	TicketCode           string
	Nickname             string
	IsAdmin              bool
	TotalPoints          int
	HiddenFromLeaderboard bool
	CreatedAt            time.Time
}

func (u *User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if len(u.TicketCode) > 8 {
		return u.TicketCode[:8] + "..."
	}
	return u.TicketCode
}

type ActivityType string

const (
	ActivityWorkshop ActivityType = "workshop"
	ActivityHidden   ActivityType = "hidden"
)

type Activity struct {
	ID            int64
	Name          string
	Description   string
	Location      string
	Type          ActivityType
	CreatedBy     int64
	Enabled       bool
	Points        *int // nil = use config default
	Token         string
	CreatedAt     time.Time
	CreatorBonus  bool   // workshop creator gets a percentage of scan points
	ScanMessage   string // custom message shown on successful scan (hidden activities)
}

type Scan struct {
	ID         int64
	UserID     int64
	ActivityID int64
	ScannedAt  time.Time
}

type Config struct {
	GamingThreshold           int
	GamingThresholdMode       string // "absolute" or "multiplier"
	GamingThresholdMultiplier int
	DefaultWorkshopPoints     int
	DefaultHiddenPoints       int
	PenaltyPoints             int
	UsePretixCheckin          bool   // use Pretix checked-in count for multiplier (default: local user count)
	RequirePretixLogin        bool   // require Pretix ticket verification at login (default: true)
	TicketCodeRegex           string // regex for ticket code validation (default: ^[A-Z0-9]{5}$)
	CreatorBonusPercentage    int    // percentage of scan points awarded to workshop creator (0-100)
}

type LastScanInfo struct {
	UserDisplayName string
	ScannedAt       time.Time
}

type LeaderboardEntry struct {
	UserID        int64
	Rank          int
	DisplayName   string
	TotalPoints   int
	IsCurrentUser bool
}
