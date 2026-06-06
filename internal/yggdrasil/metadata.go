package yggdrasil

type Metadata struct {
	Meta         MetadataInfo `json:"meta"`
	SkinDomains  []string     `json:"skinDomains"`
	SignatureKey string       `json:"signaturePublickey,omitempty"`
}

type MetadataInfo struct {
	ServerName            string `json:"serverName"`
	ImplementationName    string `json:"implementationName"`
	ImplementationVersion string `json:"implementationVersion"`
	Links                 Links  `json:"links"`
}

type Links struct {
	Homepage string `json:"homepage"`
	Register string `json:"register"`
}

func DefaultMetadata(publicBaseURL string) Metadata {
	return Metadata{
		Meta: MetadataInfo{
			ServerName:            "Authman",
			ImplementationName:    "authman",
			ImplementationVersion: "0.1.0-dev",
			Links: Links{
				Homepage: publicBaseURL,
				Register: publicBaseURL,
			},
		},
		SkinDomains: []string{
			".minecraft.net",
			".mojang.com",
			"textures.minecraft.net",
		},
	}
}
