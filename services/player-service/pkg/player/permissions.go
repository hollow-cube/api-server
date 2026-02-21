package player

import "strconv"

type Flags uint64

// Has is a utility for checking if a string flag set (eg from an api call) has a particular flag.
func Has(flags string, flag Flags) bool {
	fs, _ := strconv.ParseUint(flags, 10, 64)
	return fs&uint64(flag) == uint64(flag)
}

func (f Flags) Has(flag Flags) bool {
	return f&flag == flag
}

const (
	FlagExtendedLimits Flags = 1 << iota
	FlagBypassWhitelist

	FlagMapDelete

	FlagBan
)

type Role string

const (
	// This type is tied to the DB column on player data, it must be kept in sync.

	// zeroRole is not a db type, just left to indicate that we use zero initialization (to 0 flags) sometimes.
	zeroRole Role = ""

	DefaultRole   Role = "default"
	HypercubeRole Role = "hypercube"
	MediaRole     Role = "media"

	CT1Role  Role = "ct_1"
	Mod1Role Role = "mod_1"
	Dev1Role Role = "dev_1"

	CT2Role  Role = "ct_2"
	Mod2Role Role = "mod_2"
	Dev2Role Role = "dev_2"

	CT3Role  Role = "ct_3"
	Mod3Role Role = "mod_3"
	Dev3Role Role = "dev_3"

	hypercubeFlags = FlagExtendedLimits
	mediaFlags     = hypercubeFlags
	adminFlags     = hypercubeFlags
)

func (r Role) Flags() Flags {
	switch r {
	case HypercubeRole:
		return hypercubeFlags
	case MediaRole:
		return mediaFlags
	case CT1Role, Mod1Role, Dev1Role,
		CT2Role, Mod2Role, Dev2Role,
		CT3Role, Mod3Role, Dev3Role:
		return adminFlags
	default:
		return 0
	}
}

func (r Role) Color() string {
	switch r {
	case HypercubeRole:
		return "#ffb700"
	case MediaRole:
		return "#cc39e9"
	case CT1Role, Mod1Role, Dev1Role:
		return "#46FA32"
	case CT2Role, Mod2Role, Dev2Role:
		return "#30FBFF"
	case CT3Role, Mod3Role, Dev3Role:
		return "#fa4141"
	default:
		return ""
	}
}

func (r Role) Badge() string {
	switch r {
	case HypercubeRole:
		return "hypercube/gold"
	case MediaRole,
		CT1Role, Mod1Role, Dev1Role,
		CT2Role, Mod2Role, Dev2Role,
		CT3Role, Mod3Role, Dev3Role:
		return string(r)
	default:
		return ""
	}
}
