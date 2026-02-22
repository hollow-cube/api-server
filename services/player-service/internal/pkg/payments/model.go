package payments

import (
	"time"
)

var (
	tbWebstoreId = "tc32-b931f31ef8a34e3b5e2b9ec6ea9669d2114e8075"
	// Tebex package IDs for hypercubes and cubits
	CubitsPackages = map[int]struct {
		name   string
		Amount int
	}{
		6018804: {"cubits_50", 50},
		6020095: {"cubits_105", 105},
		6020096: {"cubits_220", 220},
		6020097: {"cubits_400", 400},
		6020099: {"cubits_600", 600},
	}
	HypercubePackages = map[int]struct {
		name     string
		Duration time.Duration
	}{
		// 1 day longer because tebex seems to have a weird definition of month
		// and it expires before we get the next payment event.
		6018860: {"hypercube_1mo", 32 * 24 * time.Hour},
		6282911: {"hypercube_1y", 366 * 24 * time.Hour},
	}
	PackageNameMap map[string]int // filled by init func
)

func init() {
	PackageNameMap = make(map[string]int)
	for id, pkg := range CubitsPackages {
		PackageNameMap[pkg.name] = id
	}
	for id, pkg := range HypercubePackages {
		PackageNameMap[pkg.name] = id
	}
}
