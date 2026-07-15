package versions

var releaseOverrides = []Version{
	{
		MinecraftVersion: "26.1.2",
		Protocol:         775,
		MajorVersion:     "26.1",
		ReleaseType:      "release",
		HasProtocol:      true,
		HasLoginPacket:   true,
	},
}

func init() {
	for _, version := range releaseOverrides {
		allVersions = append(allVersions, version)
		byProtocol[version.Protocol] = append(byProtocol[version.Protocol], version)
	}
}
