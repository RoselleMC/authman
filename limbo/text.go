package limbgo

import (
	"bytes"
	"fmt"

	"github.com/RoselleMC/authman/limbo/minimessage"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec"
)

// MiniMessageParser parses MiniMessage text into Minecraft rich text.
type MiniMessageParser interface {
	ParseMiniMessage(input string) (component.Component, error)
}

// MiniMessageParserFunc adapts a function to MiniMessageParser.
type MiniMessageParserFunc func(string) (component.Component, error)

// ParseMiniMessage implements MiniMessageParser.
func (fn MiniMessageParserFunc) ParseMiniMessage(input string) (component.Component, error) {
	return fn(input)
}

type defaultMiniMessageParser struct{}

// DefaultMiniMessageParser is the parser used by ParseMiniMessage.
var DefaultMiniMessageParser MiniMessageParser = defaultMiniMessageParser{}

// ParseMiniMessage parses MiniMessage text with the default parser.
func ParseMiniMessage(input string) (component.Component, error) {
	return DefaultMiniMessageParser.ParseMiniMessage(input)
}

func (defaultMiniMessageParser) ParseMiniMessage(input string) (out component.Component, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			out = nil
			err = fmt.Errorf("parse minimessage: %v", recovered)
		}
	}()
	return minimessage.Parse(input)
}

// MarshalComponentJSON serializes a rich text component for Minecraft packets.
func MarshalComponentJSON(protocol int32, message component.Component) ([]byte, error) {
	if message == nil {
		message = &component.Text{}
	}
	var out bytes.Buffer
	encoder := codec.Json{
		UseLegacyFieldNames:                     protocol < 770,
		UseLegacyClickEventStructure:            protocol < 770,
		UseLegacyHoverEventStructure:            protocol < 770,
		EmitChangePageClickEventPageAsString:    protocol < 771,
		EmitCompactTextComponent:                false,
		EmitHoverShowEntityIdAsIntArray:         protocol >= 764,
		EmitHoverShowEntityKeyAsTypeAndUuidAsId: protocol < 770,
		EmitDefaultItemHoverQuantity:            protocol >= 766,
		NoDownsampleColor:                       true,
		StdJson:                                 true,
	}
	if err := encoder.Marshal(&out, message); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
