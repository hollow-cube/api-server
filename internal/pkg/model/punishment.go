package model

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/playerdb"
)

const PermanentBanMessage = `
<bold><red>You are permanently banned from the server</red></bold>

<white><strikethrough>                 </strikethrough> [ ɪɴꜰᴏ ] <strikethrough>                 </strikethrough></white>

<gray>ʀᴇᴀꜱᴏɴ: <white>%s</white></gray>

<white><strikethrough>                </strikethrough> [ ʟɪɴᴋꜱ ] <strikethrough>                </strikethrough></white>

<gray>ᴅɪꜱᴄᴏʀᴅ: <click:open_url:'https://discord.hollowcube.net'><underlined><blue>discord.hollowcube.net</blue></underlined></click></gray>
<gray>ᴡᴇʙꜱɪᴛᴇ: <click:open_url:'https://hollowcube.net'><underlined><blue>hollowcube.net</blue></underlined></click></gray>
`
const TemporaryBanMessage = `
<bold><red>You are temporarily banned from the server</red></bold>

<white><strikethrough>                 </strikethrough> [ ɪɴꜰᴏ ] <strikethrough>                 </strikethrough></white>

<gray>ʀᴇᴀꜱᴏɴ: <white>%s</white></gray>
<gray>ᴇxᴘɪʀᴇꜱ ɪɴ: <white>%s</white></gray>

<white><strikethrough>                </strikethrough> [ ʟɪɴᴋꜱ ] <strikethrough>                </strikethrough></white>

<gray>ᴅɪꜱᴄᴏʀᴅ: <click:open_url:'https://discord.hollowcube.net'><underlined><blue>discord.hollowcube.net</blue></underlined></click></gray>
<gray>ᴡᴇʙꜱɪᴛᴇ: <click:open_url:'https://hollowcube.net'><underlined><blue>hollowcube.net</blue></underlined></click></gray>
`

type PunishmentType string

const (
	PunishmentTypeBan  PunishmentType = "ban"
	PunishmentTypeKick PunishmentType = "kick"
	PunishmentTypeMute PunishmentType = "mute"
)

type Punishment struct {
	Id         int            `json:"id"`         // The ID of the punishment
	PlayerId   string         `json:"playerId"`   // The player who is punished
	ExecutorId string         `json:"executorId"` // The player who did the punishing
	Type       PunishmentType `json:"type"`       // The type of punishment
	CreatedAt  time.Time      `json:"createdAt"`  // The time the punishment was added
	LadderId   *string        `json:"ladderId"`   // The relevant punishment ladder, nil for type=kick
	Comment    string         `json:"comment"`    // The provided reason for the punishment
	ExpiresAt  *time.Time     `json:"expiresAt"`  // The time the punishment will expire, or missing for permanent.

	// If any of these are present all of them are, and it means the punishment is no longer active.
	RevokedBy     *string    `json:"revokedBy"`     // The player who revoked the punishment
	RevokedAt     *time.Time `json:"revokedAt"`     // The time the punishment was revoked
	RevokedReason *string    `json:"revokedReason"` // The reason the punishment was revoked
}

type PunishmentLadder struct {
	Id      string
	Name    string
	Type    PunishmentType
	Entries []*PunishmentLadderEntry
	Reasons []*PunishmentReason
}

type PunishmentReason struct {
	Id      string
	Aliases []string
}

type PunishmentLadderEntry struct {
	Duration int64
}

type PunishmentUpdateAction int

const (
	PunishmentUpdateAction_Create PunishmentUpdateAction = iota
	PunishmentUpdateAction_Revoke
)

func (a PunishmentUpdateAction) String() string {
	return [...]string{"created", "revoked"}[a]
}

type PunishmentUpdateMessage struct {
	Action     PunishmentUpdateAction `json:"action"`
	Punishment *playerdb.Punishment   `json:"punishment"`
	Message    string                 `json:"message"`
}

func (m PunishmentUpdateMessage) Subject() string {
	return fmt.Sprintf("punishment.%v", m.Action)
}

func FormatPunishmentMessage(p *playerdb.Punishment) string {
	if p == nil {
		return "You have been punished on the server."
	} else if p.Type == "ban" {
		if p.ExpiresAt == nil {
			return fmt.Sprintf(PermanentBanMessage, p.Comment)
		}
		return fmt.Sprintf(TemporaryBanMessage, p.Comment, formatDuration(time.Until(*p.ExpiresAt)))
	} else if p.Type == "mute" {
		return fmt.Sprintf("You have been muted on the server for reason: %s", p.Comment)
	} else if p.Type == "kick" {
		return fmt.Sprintf("You have been kicked from the server for reason: %s", p.Comment)
	}
	return "You have been punished on the server for reason: " + p.Comment
}

func ConvertConfigLadders2Model(ladders []config.PunishmentLadder) (map[string]*PunishmentLadder, error) {
	result := make(map[string]*PunishmentLadder)

	for _, ladder := range ladders {
		apiLadder, err := configLadder2Model(ladder)
		if err != nil {
			return nil, fmt.Errorf("failed to convert config ladder to api ladder: %w", err)
		}

		result[apiLadder.Id] = apiLadder
	}

	return result, nil
}

func configLadder2Model(ladder config.PunishmentLadder) (*PunishmentLadder, error) {
	entries := make([]*PunishmentLadderEntry, len(ladder.Entries))
	for i, entry := range ladder.Entries {
		duration, err := convertPunishmentDurationStringToSeconds(entry.Duration)
		if err != nil {
			return nil, fmt.Errorf("failed to convert duration: %w", err)
		}

		entries[i] = &PunishmentLadderEntry{Duration: duration}
	}

	reasons := make([]*PunishmentReason, len(ladder.Reasons))
	for i2, reason := range ladder.Reasons {
		reasons[i2] = &PunishmentReason{Id: reason.Id, Aliases: reason.Aliases}
	}

	return &PunishmentLadder{
		Id:      ladder.Id,
		Name:    ladder.Name,
		Type:    PunishmentType(ladder.Type),
		Entries: entries,
		Reasons: reasons,
	}, nil
}

var durationRegex = regexp.MustCompile("(\\d+)([a-z]*)")

type durationUnit int64

var (
	// All these units are conversions to seconds
	unitSeconds durationUnit = 1
	unitMinutes durationUnit = 60 * unitSeconds
	unitHours   durationUnit = 60 * unitMinutes
	unitDays    durationUnit = 24 * unitHours
	unitWeeks   durationUnit = 7 * unitDays
	unitMonths  durationUnit = 30 * unitDays
)

// This exists because Go's default parser doesn't quite support what we want,
// notably not supporting weeks or months
func convertPunishmentDurationStringToSeconds(text string) (int64, error) {
	if text == "permanent" {
		return -1, nil
	}

	match := durationRegex.FindStringSubmatch(text)
	if match == nil {
		return 0, fmt.Errorf("invalid duration string: %s", text)
	}

	// match[0] is the full match, match[1] is group 1 (the amount), match[2] is group 2 (the unit)
	amount, err := strconv.ParseInt(match[1], 10, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration amount: %w", err)
	}

	var unit durationUnit
	switch match[2] {
	case "s":
		unit = unitSeconds
	case "m":
		unit = unitMinutes
	case "h":
		unit = unitHours
	case "d":
		unit = unitDays
	case "w":
		unit = unitWeeks
	case "mo":
		unit = unitMonths
	default:
		panic("unknown unit: " + match[2])
	}

	return amount * int64(unit), nil
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	var parts []string
	if d.Hours() >= 24 {
		parts = append(parts, fmt.Sprintf("%dd", int(d.Hours()/24)))
	}
	if math.Mod(d.Hours(), 24) >= 1 {
		parts = append(parts, fmt.Sprintf("%dh", int(math.Mod(d.Hours(), 24))))
	}
	if math.Mod(d.Minutes(), 60) >= 1 {
		parts = append(parts, fmt.Sprintf("%dm", int(math.Mod(d.Minutes(), 60))))
	}
	if len(parts) == 0 && math.Mod(d.Seconds(), 60) >= 1 {
		parts = append(parts, fmt.Sprintf("%ds", int(math.Mod(d.Seconds(), 60))))
	}
	return strings.Join(parts, " ")
}
