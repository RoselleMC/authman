package versions

import "sort"

// Version is one Minecraft Java Edition version entry generated from
// minecraft-data.
type Version struct {
	MinecraftVersion string
	Protocol         int32
	MajorVersion     string
	ReleaseType      string
	HasProtocol      bool
	HasLoginPacket   bool
}

// LookupProtocol returns all known Minecraft releases for a protocol number.
//
// A protocol can map to multiple patch releases. Callers should prefer exact
// MinecraftVersion matches when they have them and otherwise use the newest
// release in this slice.
func LookupProtocol(protocol int32) []Version {
	matches := byProtocol[protocol]
	out := make([]Version, len(matches))
	copy(out, matches)
	return out
}

// All returns every generated version sorted by Minecraft release order from
// minecraft-data.
func All() []Version {
	out := make([]Version, len(allVersions))
	copy(out, allVersions)
	return out
}

// LatestRelease returns the newest generated release version.
func LatestRelease() (Version, bool) {
	for i := len(allVersions) - 1; i >= 0; i-- {
		if allVersions[i].ReleaseType == "" || allVersions[i].ReleaseType == "release" {
			return allVersions[i], true
		}
	}
	return Version{}, false
}

// Protocols returns generated protocol numbers in ascending order.
func Protocols() []int32 {
	protocols := make([]int32, 0, len(byProtocol))
	for protocol := range byProtocol {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool {
		return protocols[i] < protocols[j]
	})
	return protocols
}
