package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/RoselleMC/limbgo"
	"github.com/RoselleMC/limbgo/world/schematic"
)

const maxLimboBlueprintUploadBytes = 32 * 1024 * 1024

type limboBlueprintRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
}

func (s *Server) handleAdminLimboBlueprints(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	blueprints := s.store.ListLimboBlueprints(r.Context())
	filtered := make([]store.LimboBlueprint, 0, len(blueprints))
	for _, blueprint := range blueprints {
		if search != "" && !containsFold(blueprint.ID, search) && !containsFold(blueprint.Name, search) && !containsFold(blueprint.Description, search) && !containsFold(blueprint.Filename, search) {
			continue
		}
		filtered = append(filtered, blueprint)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		cmp := 0
		switch sortKey {
		case "name":
			cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "blocks":
			cmp = compareInts(blueprintBlockCount(a), blueprintBlockCount(b))
		case "size":
			cmp = compareInts64(a.SizeBytes, b.SizeBytes)
		case "dimension":
			cmp = strings.Compare(fmt.Sprint(a.Config["dimension"]), fmt.Sprint(b.Config["dimension"]))
		case "updated":
			cmp = compareTime(a.UpdatedAt, b.UpdatedAt)
		default:
			cmp = compareTime(b.UpdatedAt, a.UpdatedAt)
		}
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, blueprint := range filtered[start:end] {
		data = append(data, limboBlueprintData(blueprint, false))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
}

func (s *Server) handleAdminLimboBlueprintDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	blueprint, err := s.store.GetLimboBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "limbo_blueprint.not_found", "limbo blueprint not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, limboBlueprintData(blueprint, false), nil)
}

func (s *Server) handleAdminUploadLimboBlueprint(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	if err := r.ParseMultipartForm(maxLimboBlueprintUploadBytes); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.upload_invalid", "invalid blueprint upload"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.file_required", "schematic file is required"))
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxLimboBlueprintUploadBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxLimboBlueprintUploadBytes {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.file_invalid", "schematic file is invalid or too large"))
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(header.Filename, ".schem")
	}
	description := strings.TrimSpace(r.FormValue("description"))
	config := defaultLimboBlueprintConfig("", map[string]any{})
	if rawConfig := strings.TrimSpace(r.FormValue("config")); rawConfig != "" {
		var decoded map[string]any
		err := json.Unmarshal([]byte(rawConfig), &decoded)
		if err == nil {
			config = defaultLimboBlueprintConfig("", decoded)
		}
	}
	blueprint, apiErr := buildLimboBlueprint("", name, description, header.Filename, header.Header.Get("Content-Type"), raw, config)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	blueprint, err = s.store.UpsertLimboBlueprint(r.Context(), blueprint)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, blueprint.ID, "limbo_blueprint.upload", map[string]any{"name": blueprint.Name, "sha256": blueprint.SHA256})
	s.pushAllNodeSync(r.Context(), "limbo_blueprint.upload")
	api.WriteJSON(w, http.StatusCreated, limboBlueprintData(blueprint, false), nil)
}

func (s *Server) handleAdminUpdateLimboBlueprint(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	existing, err := s.store.GetLimboBlueprint(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "limbo_blueprint.not_found", "limbo blueprint not found"))
		return
	}
	var req limboBlueprintRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.name_required", "blueprint name is required"))
		return
	}
	config := defaultLimboBlueprintConfig(existing.ID, req.Config)
	blueprint, apiErr := buildLimboBlueprint(existing.ID, name, req.Description, existing.Filename, existing.ContentType, existing.Schematic, config)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	blueprint, err = s.store.UpsertLimboBlueprint(r.Context(), blueprint)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_blueprint.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, blueprint.ID, "limbo_blueprint.update", map[string]any{"name": blueprint.Name})
	s.pushAllNodeSync(r.Context(), "limbo_blueprint.update")
	api.WriteJSON(w, http.StatusOK, limboBlueprintData(blueprint, false), nil)
}

func (s *Server) handleAdminDeleteLimboBlueprint(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := s.store.DeleteLimboBlueprint(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "limbo_blueprint.not_found", "limbo blueprint not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, id, "limbo_blueprint.delete", nil)
	s.pushAllNodeSync(r.Context(), "limbo_blueprint.delete")
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleNodeLimboBlueprint(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !strings.EqualFold(n.Mode, "limbo_portal") {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo nodes can fetch blueprints"))
		return
	}
	blueprint, err := s.store.GetLimboBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "limbo_blueprint.not_found", "limbo blueprint not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, limboBlueprintData(blueprint, true), nil)
}

func buildLimboBlueprint(id, name, description, filename, contentType string, raw []byte, config map[string]any) (store.LimboBlueprint, *api.Error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.LimboBlueprint{}, api.NewError(http.StatusBadRequest, "limbo_blueprint.name_required", "blueprint name is required")
	}
	worldID := stringFromAnyServer(config["world_id"])
	if worldID == "" {
		if id != "" {
			worldID = "authman-" + id
		} else {
			worldID = "authman-blueprint"
		}
	}
	dimension := dimensionFromConfig(config)
	world, err := schematic.Load(bytes.NewReader(raw), schematic.Options{WorldID: worldID, Dimension: dimension})
	if err != nil {
		return store.LimboBlueprint{}, api.NewError(http.StatusBadRequest, "limbo_blueprint.schematic_invalid", fmt.Sprintf("invalid schematic: %v", err))
	}
	preview := limboBlueprintPreview(world)
	config = defaultLimboBlueprintConfig(world.ID(), config)
	if _, ok := config["spawn"]; !ok {
		config["spawn"] = defaultSpawnFromPreview(preview)
	}
	sum := sha256.Sum256(raw)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return store.LimboBlueprint{
		ID:          strings.TrimSpace(id),
		Name:        name,
		Description: strings.TrimSpace(description),
		Filename:    strings.TrimSpace(filename),
		ContentType: strings.TrimSpace(contentType),
		SizeBytes:   int64(len(raw)),
		SHA256:      hex.EncodeToString(sum[:]),
		Schematic:   append([]byte(nil), raw...),
		Preview:     preview,
		Config:      config,
	}, nil
}

func limboBlueprintData(blueprint store.LimboBlueprint, includeSchematic bool) map[string]any {
	data := map[string]any{
		"id":           blueprint.ID,
		"name":         blueprint.Name,
		"description":  blueprint.Description,
		"filename":     blueprint.Filename,
		"content_type": blueprint.ContentType,
		"size_bytes":   blueprint.SizeBytes,
		"sha256":       blueprint.SHA256,
		"preview":      blueprint.Preview,
		"config":       blueprint.Config,
		"created_at":   blueprint.CreatedAt,
		"updated_at":   blueprint.UpdatedAt,
	}
	if includeSchematic {
		data["schematic_base64"] = base64.StdEncoding.EncodeToString(blueprint.Schematic)
	}
	return data
}

func blueprintBlockCount(blueprint store.LimboBlueprint) int {
	value, ok := blueprint.Preview["block_count"]
	if !ok {
		return 0
	}
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

func defaultLimboBlueprintConfig(worldID string, input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	if strings.TrimSpace(worldID) != "" {
		out["world_id"] = strings.TrimSpace(worldID)
	} else if strings.TrimSpace(stringFromAnyServer(out["world_id"])) == "" {
		out["world_id"] = "authman-blueprint"
	}
	if strings.TrimSpace(stringFromAnyServer(out["dimension"])) == "" {
		out["dimension"] = "overworld"
	}
	if spawn, ok := out["spawn"].(map[string]any); ok {
		out["spawn"] = normalizeSpawnMap(spawn)
	}
	return out
}

func dimensionFromConfig(config map[string]any) limbgo.Dimension {
	switch strings.ToLower(strings.TrimSpace(stringFromAnyServer(config["dimension"]))) {
	case "nether":
		return limbgo.DimensionPreset(limbgo.DimensionNether, 0)
	case "end":
		return limbgo.DimensionPreset(limbgo.DimensionEnd, 0)
	default:
		return limbgo.DimensionPreset(limbgo.DimensionOverworld, 0)
	}
}

func normalizeSpawnMap(input map[string]any) map[string]any {
	return map[string]any{
		"x":     floatFromAny(input["x"], 0),
		"y":     floatFromAny(input["y"], 65),
		"z":     floatFromAny(input["z"], 0),
		"yaw":   floatFromAny(input["yaw"], 0),
		"pitch": floatFromAny(input["pitch"], 0),
	}
}

func floatFromAny(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func limboBlueprintPreview(world *limbgo.MemoryWorld) map[string]any {
	palette := world.BlockPalette()
	paletteData := make([]map[string]any, 0, len(palette))
	for id, state := range palette {
		paletteData = append(paletteData, map[string]any{"id": id, "name": state.Name})
	}
	sort.Slice(paletteData, func(i, j int) bool {
		return intFromPreview(paletteData[i]["id"]) < intFromPreview(paletteData[j]["id"])
	})
	blocks := make([]map[string]any, 0)
	total := 0
	minX, minY, minZ := int32(0), int32(0), int32(0)
	maxX, maxY, maxZ := int32(0), int32(0), int32(0)
	first := true
	for _, chunk := range world.Chunks {
		for _, section := range chunk.Sections {
			for index, paletteID := range section.BlockStateIDs {
				if paletteID == 0 {
					continue
				}
				localX := int32(index & 15)
				localZ := int32((index >> 4) & 15)
				localY := int32((index >> 8) & 15)
				x := chunk.X*16 + localX
				y := section.Y*16 + localY
				z := chunk.Z*16 + localZ
				if first {
					minX, maxX, minY, maxY, minZ, maxZ = x, x, y, y, z, z
					first = false
				} else {
					if x < minX {
						minX = x
					}
					if x > maxX {
						maxX = x
					}
					if y < minY {
						minY = y
					}
					if y > maxY {
						maxY = y
					}
					if z < minZ {
						minZ = z
					}
					if z > maxZ {
						maxZ = z
					}
				}
				total++
				name := ""
				if int(paletteID) < len(palette) {
					name = palette[paletteID].Name
				}
				blocks = append(blocks, map[string]any{"x": x, "y": y, "z": z, "p": paletteID, "name": name})
			}
		}
	}
	bounds := map[string]any{
		"min_x": minX, "min_y": minY, "min_z": minZ,
		"max_x": maxX, "max_y": maxY, "max_z": maxZ,
		"width": maxX - minX + 1, "height": maxY - minY + 1, "length": maxZ - minZ + 1,
	}
	return map[string]any{
		"bounds":      bounds,
		"block_count": total,
		"sampled":     len(blocks),
		"palette":     paletteData,
		"blocks":      blocks,
	}
}

func defaultSpawnFromPreview(preview map[string]any) map[string]any {
	bounds, _ := preview["bounds"].(map[string]any)
	return map[string]any{
		"x":     floatFromAny(bounds["min_x"], 0) + floatFromAny(bounds["width"], 1)/2,
		"y":     floatFromAny(bounds["max_y"], 64) + 1,
		"z":     floatFromAny(bounds["min_z"], 0) + floatFromAny(bounds["length"], 1)/2,
		"yaw":   0,
		"pitch": 0,
	}
}

func intFromPreview(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case uint32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
