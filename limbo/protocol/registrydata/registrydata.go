package registrydata

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	maxRegistryArchiveBytes = 32 << 20
	maxRegistryEntryBytes   = 32 << 20
	maxRegistryExpanded     = 128 << 20
	maxRegistryFiles        = 512
)

//go:embed registrydata.zip
var embeddedZip []byte

type Registry struct {
	ID      string
	Entries []Entry
}

type TagRegistry struct {
	ID   string
	Tags []Tag
}

type Tag struct {
	Key    string
	Values []int32
}

type Entry struct {
	Key   string
	Value []byte
}

type Data struct {
	registries map[int32][]Registry
	tags       map[int32][]TagRegistry
	codecs     map[int32][]byte
	dimensions map[int32][]byte
}

// Source returns a registry data snapshot for a new connection.
type Source interface {
	RegistryData() (*Data, error)
}

type encodedData struct {
	Registries      map[string][]encodedRegistry    `json:"registries"`
	Tags            map[string][]encodedTagRegistry `json:"tags,omitempty"`
	DimensionCodecs map[string]string               `json:"dimension_codecs"`
	Dimensions      map[string]string               `json:"dimensions"`
}

type encodedProtocolData struct {
	FormatVersion  int                  `json:"format_version,omitempty"`
	Protocol       int32                `json:"protocol,omitempty"`
	Registries     []encodedRegistry    `json:"registries,omitempty"`
	Tags           []encodedTagRegistry `json:"tags,omitempty"`
	DimensionCodec string               `json:"dimension_codec,omitempty"`
	Dimension      string               `json:"dimension,omitempty"`
}

type encodedRegistry struct {
	ID      string         `json:"id"`
	Entries []encodedEntry `json:"entries"`
}

type encodedEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type encodedTagRegistry struct {
	ID   string       `json:"id"`
	Tags []encodedTag `json:"tags"`
}

type encodedTag struct {
	Key    string  `json:"key"`
	Values []int32 `json:"values"`
}

var (
	defaultOnce sync.Once
	defaultData *Data
	defaultErr  error
)

func Registries(protocol int32) ([]Registry, bool) {
	data, err := Default()
	if err != nil {
		return nil, false
	}
	registries, ok := data.Registries(protocol)
	return registries, ok
}

func Tags(protocol int32) ([]TagRegistry, bool) {
	data, err := Default()
	if err != nil {
		return nil, false
	}
	tags, ok := data.Tags(protocol)
	return tags, ok
}

func DimensionCodec(protocol int32) ([]byte, bool) {
	data, err := Default()
	if err != nil {
		return nil, false
	}
	codec, ok := data.DimensionCodec(protocol)
	return codec, ok
}

func Default() (*Data, error) {
	defaultOnce.Do(func() {
		defaultData, defaultErr = LoadZipBytes(embeddedZip)
	})
	return defaultData, defaultErr
}

func (d *Data) RegistryData() (*Data, error) {
	if d == nil {
		return nil, fmt.Errorf("registry data is nil")
	}
	return d, nil
}

func LoadFile(path string) (*Data, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isZip(data) {
		return LoadZipBytes(data)
	}
	return LoadBytes(data)
}

func LoadZipFile(path string) (*Data, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadZipBytes(data)
}

func LoadBytes(raw []byte) (*Data, error) {
	var encoded encodedData
	if err := decodeStrictJSON(raw, &encoded); err != nil {
		return nil, fmt.Errorf("parse registry data: %w", err)
	}
	out := newData()
	for rawProtocol, encodedRegistries := range encoded.Registries {
		protocol, err := parseProtocol(rawProtocol)
		if err != nil {
			return nil, err
		}
		if err := out.decodeRegistries(protocol, encodedRegistries); err != nil {
			return nil, err
		}
	}
	for rawProtocol, encodedTagRegistries := range encoded.Tags {
		protocol, err := parseProtocol(rawProtocol)
		if err != nil {
			return nil, err
		}
		if err := out.decodeTags(protocol, encodedTagRegistries); err != nil {
			return nil, err
		}
	}
	for rawProtocol, rawCodec := range encoded.DimensionCodecs {
		protocol, err := parseProtocol(rawProtocol)
		if err != nil {
			return nil, err
		}
		if err := out.decodeDimensionCodec(protocol, rawCodec); err != nil {
			return nil, err
		}
	}
	for rawProtocol, rawDimension := range encoded.Dimensions {
		protocol, err := parseProtocol(rawProtocol)
		if err != nil {
			return nil, err
		}
		if err := out.decodeDimension(protocol, rawDimension); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func LoadZipBytes(raw []byte) (*Data, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("registry data zip is empty")
	}
	if len(raw) > maxRegistryArchiveBytes {
		return nil, fmt.Errorf("registry data zip is %d bytes; maximum is %d", len(raw), maxRegistryArchiveBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open registry data zip: %w", err)
	}
	if len(reader.File) > maxRegistryFiles {
		return nil, fmt.Errorf("registry data zip has %d entries; maximum is %d", len(reader.File), maxRegistryFiles)
	}
	out := newData()
	loaded := 0
	seenProtocols := make(map[int32]string)
	var expanded uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("registry data zip contains unsafe entry %q", file.Name)
		}
		if filepath.Ext(name) != ".json" {
			return nil, fmt.Errorf("registry data zip contains unsupported entry %q", file.Name)
		}
		if file.UncompressedSize64 > maxRegistryEntryBytes {
			return nil, fmt.Errorf("registry data entry %q expands to %d bytes; maximum is %d", file.Name, file.UncompressedSize64, maxRegistryEntryBytes)
		}
		expanded += file.UncompressedSize64
		if expanded > maxRegistryExpanded {
			return nil, fmt.Errorf("registry data zip expands to more than %d bytes", maxRegistryExpanded)
		}
		protocol, err := parseProtocol(protocolName(file.Name))
		if err != nil {
			return nil, fmt.Errorf("parse registry zip entry %q: %w", file.Name, err)
		}
		if previous, duplicate := seenProtocols[protocol]; duplicate {
			return nil, fmt.Errorf("registry zip entries %q and %q both declare protocol %d", previous, file.Name, protocol)
		}
		if err := out.loadZipEntry(protocol, file); err != nil {
			return nil, fmt.Errorf("load registry zip entry %q: %w", file.Name, err)
		}
		seenProtocols[protocol] = file.Name
		loaded++
	}
	if loaded == 0 {
		return nil, fmt.Errorf("registry data zip contains no protocol json files")
	}
	return out, nil
}

func newData() *Data {
	return &Data{
		registries: map[int32][]Registry{},
		tags:       map[int32][]TagRegistry{},
		codecs:     map[int32][]byte{},
		dimensions: map[int32][]byte{},
	}
}

func (d *Data) loadZipEntry(protocol int32, file *zip.File) error {
	handle, err := file.Open()
	if err != nil {
		return err
	}
	defer handle.Close()
	raw, err := io.ReadAll(io.LimitReader(handle, maxRegistryEntryBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxRegistryEntryBytes {
		return fmt.Errorf("registry data entry exceeds %d bytes", maxRegistryEntryBytes)
	}
	var encoded encodedProtocolData
	if err := decodeStrictJSON(raw, &encoded); err != nil {
		return fmt.Errorf("parse protocol %d registry data: %w", protocol, err)
	}
	if encoded.FormatVersion != 0 && encoded.FormatVersion != 1 {
		return fmt.Errorf("unsupported protocol %d registry data format %d", protocol, encoded.FormatVersion)
	}
	if encoded.Protocol != 0 && encoded.Protocol != protocol {
		return fmt.Errorf("protocol file declared protocol %d, want %d", encoded.Protocol, protocol)
	}
	if err := d.decodeRegistries(protocol, encoded.Registries); err != nil {
		return err
	}
	if err := d.decodeTags(protocol, encoded.Tags); err != nil {
		return err
	}
	if err := d.decodeDimensionCodec(protocol, encoded.DimensionCodec); err != nil {
		return err
	}
	if err := d.decodeDimension(protocol, encoded.Dimension); err != nil {
		return err
	}
	return nil
}

func (d *Data) decodeRegistries(protocol int32, encodedRegistries []encodedRegistry) error {
	seenRegistries := make(map[string]struct{}, len(encodedRegistries))
	for _, encodedRegistry := range encodedRegistries {
		if strings.TrimSpace(encodedRegistry.ID) == "" {
			return fmt.Errorf("registry protocol %d has an empty registry id", protocol)
		}
		if _, duplicate := seenRegistries[encodedRegistry.ID]; duplicate {
			return fmt.Errorf("registry protocol %d repeats registry %q", protocol, encodedRegistry.ID)
		}
		seenRegistries[encodedRegistry.ID] = struct{}{}
		registry := Registry{ID: encodedRegistry.ID}
		seenEntries := make(map[string]struct{}, len(encodedRegistry.Entries))
		for _, encodedEntry := range encodedRegistry.Entries {
			if strings.TrimSpace(encodedEntry.Key) == "" {
				return fmt.Errorf("registry protocol %d %s has an empty entry key", protocol, encodedRegistry.ID)
			}
			if _, duplicate := seenEntries[encodedEntry.Key]; duplicate {
				return fmt.Errorf("registry protocol %d %s repeats entry %q", protocol, encodedRegistry.ID, encodedEntry.Key)
			}
			seenEntries[encodedEntry.Key] = struct{}{}
			value, err := base64.StdEncoding.DecodeString(encodedEntry.Value)
			if err != nil {
				return fmt.Errorf("decode registry %d %s/%s: %w", protocol, encodedRegistry.ID, encodedEntry.Key, err)
			}
			registry.Entries = append(registry.Entries, Entry{
				Key:   encodedEntry.Key,
				Value: value,
			})
		}
		d.registries[protocol] = append(d.registries[protocol], registry)
	}
	return nil
}

func (d *Data) decodeTags(protocol int32, encodedTagRegistries []encodedTagRegistry) error {
	seenRegistries := make(map[string]struct{}, len(encodedTagRegistries))
	for _, encodedTagRegistry := range encodedTagRegistries {
		if strings.TrimSpace(encodedTagRegistry.ID) == "" {
			return fmt.Errorf("registry tags protocol %d has an empty registry id", protocol)
		}
		if _, duplicate := seenRegistries[encodedTagRegistry.ID]; duplicate {
			return fmt.Errorf("registry tags protocol %d repeats registry %q", protocol, encodedTagRegistry.ID)
		}
		seenRegistries[encodedTagRegistry.ID] = struct{}{}
		tagRegistry := TagRegistry{ID: encodedTagRegistry.ID}
		seenTags := make(map[string]struct{}, len(encodedTagRegistry.Tags))
		for _, encodedTag := range encodedTagRegistry.Tags {
			if strings.TrimSpace(encodedTag.Key) == "" {
				return fmt.Errorf("registry tags protocol %d %s has an empty tag key", protocol, encodedTagRegistry.ID)
			}
			if _, duplicate := seenTags[encodedTag.Key]; duplicate {
				return fmt.Errorf("registry tags protocol %d %s repeats tag %q", protocol, encodedTagRegistry.ID, encodedTag.Key)
			}
			seenTags[encodedTag.Key] = struct{}{}
			for _, value := range encodedTag.Values {
				if value < 0 {
					return fmt.Errorf("registry tags protocol %d %s/%s has negative value %d", protocol, encodedTagRegistry.ID, encodedTag.Key, value)
				}
			}
			values := append([]int32(nil), encodedTag.Values...)
			tagRegistry.Tags = append(tagRegistry.Tags, Tag{
				Key:    encodedTag.Key,
				Values: values,
			})
		}
		d.tags[protocol] = append(d.tags[protocol], tagRegistry)
	}
	return nil
}

func (d *Data) decodeDimensionCodec(protocol int32, rawCodec string) error {
	if rawCodec == "" {
		return nil
	}
	codec, err := base64.StdEncoding.DecodeString(rawCodec)
	if err != nil {
		return fmt.Errorf("decode dimension codec %d: %w", protocol, err)
	}
	d.codecs[protocol] = codec
	return nil
}

func (d *Data) decodeDimension(protocol int32, rawDimension string) error {
	if rawDimension == "" {
		return nil
	}
	dimension, err := base64.StdEncoding.DecodeString(rawDimension)
	if err != nil {
		return fmt.Errorf("decode dimension %d: %w", protocol, err)
	}
	d.dimensions[protocol] = dimension
	return nil
}

func isZip(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04})
}

func protocolName(name string) string {
	base := filepath.Base(name)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (d *Data) Registries(protocol int32) ([]Registry, bool) {
	registries, ok := d.registries[protocol]
	return registries, ok
}

func (d *Data) Tags(protocol int32) ([]TagRegistry, bool) {
	tags, ok := d.tags[protocol]
	return tags, ok
}

func (d *Data) DimensionCodec(protocol int32) ([]byte, bool) {
	codec, ok := d.codecs[protocol]
	return codec, ok
}

func (d *Data) Dimension(protocol int32) ([]byte, bool) {
	dimension, ok := d.dimensions[protocol]
	return dimension, ok
}

func parseProtocol(value string) (int32, error) {
	protocol, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse protocol %q: %w", value, err)
	}
	return int32(protocol), nil
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
