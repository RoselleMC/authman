package limbo

import (
	"bytes"
	"net"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
)

func legacyDimensionID(dimension limbgo.Dimension) byte {
	switch limbgo.NormalizeDimension(dimension, 256).Environment {
	case limbgo.DimensionNether:
		return 255
	case limbgo.DimensionEnd:
		return 1
	default:
		return 0
	}
}

func legacyDimensionInt(dimension limbgo.Dimension) int32 {
	switch limbgo.NormalizeDimension(dimension, 256).Environment {
	case limbgo.DimensionNether:
		return -1
	case limbgo.DimensionEnd:
		return 1
	default:
		return 0
	}
}

func worldAgeAndTime(dimension limbgo.Dimension) (int64, int64) {
	dimension = limbgo.NormalizeDimension(dimension, 256)
	timeOfDay := int64(0)
	if dimension.FixedTime != nil {
		timeOfDay = *dimension.FixedTime
	}
	if dimension.TimeOfDay != nil {
		timeOfDay = *dimension.TimeOfDay
	}
	return dimension.WorldAge, timeOfDay
}

func writeUpdateTime(protocol int32, conn net.Conn, world limbgo.World, packetTables ...*packetid.Table) error {
	dimension := limbgo.NormalizeDimension(world.Dimension(), 256)
	if dimension.FixedTime == nil && dimension.TimeOfDay == nil && dimension.WorldAge == 0 {
		return nil
	}
	var packetIDs *packetid.Table
	if len(packetTables) > 0 {
		packetIDs = packetTables[0]
	}
	id, ok := resolvePacketID(packetIDs, protocol, packetid.StatePlay, packetid.ToClient, "update_time")
	if !ok {
		return nil
	}
	age, timeOfDay := worldAgeAndTime(dimension)
	var data bytes.Buffer
	if err := wire.WriteLong(&data, age); err != nil {
		return err
	}
	if err := wire.WriteLong(&data, timeOfDay); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}
