package skin

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/identity"
)

//go:embed assets/default/*
var assets embed.FS

type DefaultSkin struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

type TextureURLs struct {
	Skin   string
	Cape   string
	Elytra string
	Model  string
}

var defaultSkins = []DefaultSkin{
	{Name: "alex", Model: "slim"},
	{Name: "ari", Model: "slim"},
	{Name: "efe", Model: "slim"},
	{Name: "kai", Model: "slim"},
	{Name: "makena", Model: "slim"},
	{Name: "noor", Model: "slim"},
	{Name: "steve", Model: "slim"},
	{Name: "sunny", Model: "slim"},
	{Name: "zuri", Model: "slim"},
	{Name: "alex", Model: "wide"},
	{Name: "ari", Model: "wide"},
	{Name: "efe", Model: "wide"},
	{Name: "kai", Model: "wide"},
	{Name: "makena", Model: "wide"},
	{Name: "noor", Model: "wide"},
	{Name: "steve", Model: "wide"},
	{Name: "sunny", Model: "wide"},
	{Name: "zuri", Model: "wide"},
}

func DefaultForUUID(uuid identity.UUID) DefaultSkin {
	return defaultSkins[floorMod(javaUUIDHash(uuid), len(defaultSkins))]
}

func DefaultSkinPNG(name string, model string) ([]byte, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	model = normalizeModel(model)
	return assets.ReadFile(fmt.Sprintf("assets/default/%s/%s.png", model, name))
}

func DefaultSkinNames() []DefaultSkin {
	out := make([]DefaultSkin, len(defaultSkins))
	copy(out, defaultSkins)
	return out
}

func BuildTexturesProperty(profileUUID identity.UUID, profileName string, publicBaseURL string, urls TextureURLs) (identity.ProfileProperty, error) {
	base := strings.TrimRight(publicBaseURL, "/")
	if base == "" {
		return identity.ProfileProperty{}, fmt.Errorf("public base URL is required")
	}
	skinURL := absoluteURL(base, urls.Skin)
	if skinURL == "" {
		return identity.ProfileProperty{}, fmt.Errorf("skin URL is required")
	}
	model := normalizeModel(urls.Model)
	textures := map[string]any{
		"SKIN": map[string]any{"url": skinURL},
	}
	if model == "slim" {
		textures["SKIN"].(map[string]any)["metadata"] = map[string]any{"model": "slim"}
	}
	if cape := absoluteURL(base, urls.Cape); cape != "" {
		textures["CAPE"] = map[string]any{"url": cape}
	}
	if elytra := absoluteURL(base, urls.Elytra); elytra != "" {
		textures["ELYTRA"] = map[string]any{"url": elytra}
	}
	body := map[string]any{
		"timestamp":   time.Now().UnixMilli(),
		"profileId":   profileUUID.Compact(),
		"profileName": profileName,
		"textures":    textures,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return identity.ProfileProperty{}, err
	}
	return identity.ProfileProperty{Name: "textures", Value: base64.StdEncoding.EncodeToString(raw)}, nil
}

func TextureURLsFromProperty(properties []identity.ProfileProperty) TextureURLs {
	for _, property := range properties {
		if property.Name != "textures" || property.Value == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(property.Value)
		if err != nil {
			continue
		}
		var decoded struct {
			Textures map[string]struct {
				URL      string            `json:"url"`
				Metadata map[string]string `json:"metadata"`
			} `json:"textures"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			continue
		}
		out := TextureURLs{}
		if skin := decoded.Textures["SKIN"]; skin.URL != "" {
			out.Skin = skin.URL
			if skin.Metadata["model"] == "slim" {
				out.Model = "slim"
			} else {
				out.Model = "wide"
			}
		}
		out.Cape = decoded.Textures["CAPE"].URL
		out.Elytra = decoded.Textures["ELYTRA"].URL
		return out
	}
	return TextureURLs{}
}

func AvatarPNG(skinPNG []byte, scale int) ([]byte, error) {
	if scale < 1 {
		scale = 8
	}
	src, _, err := image.Decode(bytes.NewReader(skinPNG))
	if err != nil {
		return nil, err
	}
	head := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(head, head.Bounds(), src, image.Point{X: 8, Y: 8}, draw.Src)
	if src.Bounds().Dx() >= 64 && src.Bounds().Dy() >= 64 {
		draw.Draw(head, head.Bounds(), src, image.Point{X: 40, Y: 8}, draw.Over)
	}
	out := image.NewRGBA(image.Rect(0, 0, 8*scale, 8*scale))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := head.At(x, y)
			cell := image.Rect(x*scale, y*scale, (x+1)*scale, (y+1)*scale)
			draw.Draw(out, cell, &image.Uniform{C: c}, image.Point{}, draw.Src)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func normalizeModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "slim":
		return "slim"
	default:
		return "wide"
	}
}

func absoluteURL(base string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func javaUUIDHash(uuid identity.UUID) int {
	msb := int64(binary.BigEndian.Uint64(uuid[0:8]))
	lsb := int64(binary.BigEndian.Uint64(uuid[8:16]))
	return int(int32((msb >> 32) ^ msb ^ (lsb >> 32) ^ lsb))
}

func floorMod(value int, divisor int) int {
	return value - int(math.Floor(float64(value)/float64(divisor)))*divisor
}
