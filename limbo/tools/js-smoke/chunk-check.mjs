#!/usr/bin/env node

import { createRequire } from "node:module";
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const defaultVersions = [
  "1.8.8",
  "1.9",
  "1.9.2",
  "1.9.4",
  "1.10",
  "1.10.1",
  "1.10.2",
  "1.11",
  "1.11.2",
  "1.12",
  "1.12.1",
  "1.12.2",
  "1.13",
  "1.13.1",
  "1.13.2",
  "1.14",
  "1.14.1",
  "1.14.3",
  "1.14.4",
  "1.15",
  "1.15.1",
  "1.15.2",
  "1.16",
  "1.16.1",
  "1.16.2",
  "1.16.3",
  "1.16.4",
  "1.16.5",
  "1.17",
  "1.17.1",
  "1.18",
  "1.18.1",
  "1.18.2",
  "1.19",
  "1.19.2",
  "1.19.3",
  "1.19.4",
  "1.20",
  "1.20.1",
  "1.20.2",
  "1.20.3",
  "1.20.4",
  "1.20.5",
  "1.20.6",
  "1.21",
  "1.21.1",
  "1.21.3",
  "1.21.4",
  "1.21.5",
  "1.21.6",
  "1.21.8",
  "1.21.9",
  "1.21.11",
  "26.1.2",
];
const versionAliases = {
  "26.1": "1.21.11",
  "26.1.1": "1.21.11",
  "26.1.2": "1.21.11",
};
const protocolOverrides = {
  "26.1.2": 775,
};
const packetIdOverrides = {
  "26.1.2": {
    "login.toServer.login_start": 0,
    "login.toClient.success": 2,
    "login.toServer.login_acknowledged": 3,
    "configuration.toClient.finish_configuration": 3,
    "configuration.toServer.finish_configuration": 3,
    "play.toClient.map_chunk": 45,
  },
};
const versions = process.argv.slice(2);
const requestedVersions = versions.length > 0 ? versions : defaultVersions;

const require = createRequire(import.meta.url);
let createClient;
let mcDataForVersion;
try {
  ({ createClient } = require("minecraft-protocol"));
  mcDataForVersion = require("minecraft-data");
} catch {
  const depsDir = fs.mkdtempSync(path.join(os.tmpdir(), "limbgo-js-smoke-deps-"));
  fs.writeFileSync(path.join(depsDir, "package.json"), JSON.stringify({ private: true }));
  execFileSync("npm", ["install", "--silent", "minecraft-protocol@latest", "minecraft-data@latest"], {
    cwd: depsDir,
    stdio: "inherit",
  });
  const depRequire = createRequire(path.join(depsDir, "package.json"));
  ({ createClient } = depRequire("minecraft-protocol"));
  mcDataForVersion = depRequire("minecraft-data");
}

async function reservePort() {
  const srv = net.createServer();
  await new Promise((resolve, reject) => {
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", resolve);
  });
  const { port: reserved } = srv.address();
  await new Promise((resolve) => srv.close(resolve));
  return reserved;
}

function waitForServer(proc, expectedPort) {
  return new Promise((resolve, reject) => {
    let output = "";
    const timer = setTimeout(() => {
      reject(new Error(`server did not start on ${expectedPort}; output:\n${output}`));
    }, 30000);
    proc.stdout.on("data", (chunk) => {
      output += chunk.toString();
      if (output.includes(`LISTEN ${expectedPort}`)) {
        clearTimeout(timer);
        resolve();
      }
    });
    proc.stderr.on("data", (chunk) => {
      output += chunk.toString();
    });
    proc.once("exit", (code, signal) => {
      clearTimeout(timer);
      reject(new Error(`server exited before ready: code=${code} signal=${signal}; output:\n${output}`));
    });
  });
}

function checkVersion(port, version) {
	return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      finish(new Error(`${version}: timed out waiting for map_chunk`));
    }, 20000);
    let done = false;
    let client;

    function finish(err, value) {
      if (done) {
        return;
      }
      done = true;
      clearTimeout(timeout);
      if (client) {
        client.removeAllListeners("packet");
        client.removeAllListeners("error");
        client.removeAllListeners("end");
        client.end();
      }
      if (err) {
        reject(err);
      } else {
        resolve(value);
      }
    }

		try {
			client = createClient({
				host: "127.0.0.1",
				port,
				username: `Bot${version.replaceAll(".", "")}`,
				version,
				auth: "offline",
				hideErrors: true,
			});
		} catch (err) {
			clearTimeout(timeout);
			if (!versionAliases[version]) {
				reject(err);
				return;
			}
			rawCheckVersion(port, version).then(resolve, reject);
			return;
		}

    client.on("packet", (packet, meta) => {
      if (meta.name !== "map_chunk") {
        return;
      }
      try {
        const chunkData = getChunkData(packet);
        const stoneState = stoneStateFor(version);
        const got = firstBlockState(version, chunkData);
        if (got !== stoneState) {
          throw new Error(
            `${version}: first chunk block state ${got}, want stone state ${stoneState}; ` +
              `chunkData[0..24]=${chunkData.subarray(0, 24).toString("hex")}; keys=${Object.keys(packet).join(",")}`,
          );
        }
        finish(null, {
				version,
				protocol: protocolForVersion(version),
				chunkDataLength: chunkData.length,
				stoneState,
        });
      } catch (err) {
        finish(err);
      }
    });
    client.on("error", (err) => finish(err));
    client.on("end", () => {
      if (!done) {
        finish(new Error(`${version}: connection ended before map_chunk`));
      }
    });
	});
}

function rawCheckVersion(port, version) {
	return new Promise((resolve, reject) => {
		const packetData = dataForVersion(version);
		const protocol = protocolForVersion(version);
		const ids = {
			loginStart: packetId(packetData, "login", "toServer", "login_start"),
			loginSuccess: packetId(packetData, "login", "toClient", "success"),
			loginAcknowledged: packetId(packetData, "login", "toServer", "login_acknowledged"),
			finishConfigurationClient: packetId(packetData, "configuration", "toClient", "finish_configuration"),
			finishConfigurationServer: packetId(packetData, "configuration", "toServer", "finish_configuration"),
			mapChunk: packetId(packetData, "play", "toClient", "map_chunk"),
		};
		const socket = net.createConnection({ host: "127.0.0.1", port });
		let buffer = Buffer.alloc(0);
		let state = "login";
		let done = false;
		const timeout = setTimeout(() => {
			finish(new Error(`${version}: raw client timed out waiting for map_chunk`));
		}, 20000);

		function finish(err, value) {
			if (done) {
				return;
			}
			done = true;
			clearTimeout(timeout);
			socket.destroy();
			if (err) {
				reject(err);
			} else {
				resolve(value);
			}
		}

		socket.on("connect", () => {
			socket.write(packet(0, Buffer.alloc(0), { handshake: { protocol, host: "127.0.0.1", port } }));
			socket.write(packet(ids.loginStart, loginStartPayload(packetData, `Raw${version.replaceAll(".", "")}`)));
		});
		socket.on("data", (chunk) => {
			buffer = Buffer.concat([buffer, chunk]);
			try {
				for (;;) {
					const next = readFrame(buffer);
					if (!next) {
						return;
					}
					buffer = buffer.subarray(next.frameEnd);
					handleRawPacket(next.id, next.payload);
				}
			} catch (err) {
				finish(err);
			}
		});
		socket.on("error", (err) => finish(err));
		socket.on("close", () => {
			if (!done) {
				finish(new Error(`${version}: raw connection closed before map_chunk`));
			}
		});

		function handleRawPacket(id, payload) {
			if (state === "login") {
				if (id !== ids.loginSuccess) {
					return;
				}
				socket.write(packet(ids.loginAcknowledged));
				state = "configuration";
				return;
			}
			if (state === "configuration") {
				if (id !== ids.finishConfigurationClient) {
					return;
				}
				socket.write(packet(ids.finishConfigurationServer));
				state = "play";
				return;
			}
			if (state === "play" && id === ids.mapChunk) {
				const chunkData = rawModernChunkData(payload);
				const stoneState = stoneStateFor(version);
				const got = firstBlockState(version, chunkData);
				if (got !== stoneState) {
					throw new Error(`${version}: raw first chunk block state ${got}, want stone state ${stoneState}`);
				}
				finish(null, {
					version,
					protocol,
					chunkDataLength: chunkData.length,
					stoneState,
				});
			}
		}
	});
}

function packetId(data, state, direction, name) {
	const overrideKey = `${state}.${direction}.${name}`;
	const versionOverride = packetIdOverrides[data.version.minecraftVersion];
	if (versionOverride && Object.hasOwn(versionOverride, overrideKey)) {
		return versionOverride[overrideKey];
	}
  const packetType = data.protocol[state][direction].types.packet;
	const nameField = packetType[1].find((field) => field.name === "name");
	const mappings = nameField.type[1].mappings;
	for (const [rawID, packetName] of Object.entries(mappings)) {
		if (packetName === name) {
			return Number.parseInt(rawID, 16);
		}
	}
	throw new Error(`${data.version.minecraftVersion}: missing ${state}.${direction}.${name}`);
}

function packet(id, payload = Buffer.alloc(0), options = {}) {
	if (options.handshake) {
		const handshake = Buffer.concat([
			writeVarInt(options.handshake.protocol),
			writeString(options.handshake.host),
			writeU16(options.handshake.port),
			writeVarInt(2),
		]);
		return framedPacket(0, handshake);
	}
	return framedPacket(id, payload);
}

function framedPacket(id, payload) {
	const body = Buffer.concat([writeVarInt(id), payload]);
	return Buffer.concat([writeVarInt(body.length), body]);
}

function readFrame(buffer) {
	const length = readVarIntFrom(buffer, 0);
	if (!length) {
		return null;
	}
	const frameStart = length.size;
	const frameEnd = frameStart + length.value;
	if (buffer.length < frameEnd) {
		return null;
	}
	const id = readVarIntFrom(buffer, frameStart);
	if (!id) {
		return null;
	}
	return {
		id: id.value,
		payload: buffer.subarray(frameStart + id.size, frameEnd),
		frameEnd,
	};
}

function writeString(value) {
	const raw = Buffer.from(value, "utf8");
	return Buffer.concat([writeVarInt(raw.length), raw]);
}

function loginStartPayload(data, username) {
	const fields = data.protocol.login.toServer.types.packet_login_start[1];
	const parts = [];
	for (const field of fields) {
		if (field.name === "username") {
			parts.push(writeString(username));
			continue;
		}
		if (field.name === "signature") {
			parts.push(Buffer.from([0]));
			continue;
		}
		if (field.name === "playerUUID") {
			if (Array.isArray(field.type) && field.type[0] === "option") {
				parts.push(Buffer.from([1]), deterministicUUID(username));
			} else {
				parts.push(deterministicUUID(username));
			}
			continue;
		}
		throw new Error(`${data.version.minecraftVersion}: unsupported login_start field ${field.name}`);
	}
	return Buffer.concat(parts);
}

function deterministicUUID(seed) {
	const out = Buffer.alloc(16);
	const raw = Buffer.from(seed, "utf8");
	for (let i = 0; i < out.length; i++) {
		out[i] = raw[i % raw.length] ^ ((i * 31) & 0xff);
	}
	out[6] = (out[6] & 0x0f) | 0x40;
	out[8] = (out[8] & 0x3f) | 0x80;
	return out;
}

function writeU16(value) {
	const out = Buffer.alloc(2);
	out.writeUInt16BE(value);
	return out;
}

function writeVarInt(value) {
	const bytes = [];
	let remaining = value >>> 0;
	for (;;) {
		if ((remaining & ~0x7f) === 0) {
			bytes.push(remaining);
			break;
		}
		bytes.push((remaining & 0x7f) | 0x80);
		remaining >>>= 7;
	}
	return Buffer.from(bytes);
}

function readVarIntFrom(buffer, offset) {
	let value = 0;
	let shift = 0;
	for (let i = 0; i < 5; i++) {
		if (offset + i >= buffer.length) {
			return null;
		}
		const byte = buffer[offset + i];
		value |= (byte & 0x7f) << shift;
		if ((byte & 0x80) === 0) {
			return { value, size: i + 1 };
		}
		shift += 7;
	}
	throw new Error("varint too long");
}

function rawModernChunkData(payload) {
	const reader = new BufferReader(payload);
	reader.i32();
	reader.i32();
	const heightmapCount = reader.varint();
	for (let i = 0; i < heightmapCount; i++) {
		reader.varint();
		const values = reader.varint();
		for (let j = 0; j < values; j++) {
			reader.i64();
		}
	}
	const size = reader.varint();
	return reader.bytes(size);
}

function getChunkData(packet) {
  const candidate = packet.chunkData ?? packet.data;
  if (Buffer.isBuffer(candidate)) {
    return candidate;
  }
  if (candidate && Buffer.isBuffer(candidate.data)) {
    return candidate.data;
  }
  if (Array.isArray(candidate)) {
    return Buffer.from(candidate);
  }
	throw new Error(`map_chunk packet has no Buffer chunkData; keys=${Object.keys(packet).join(",")}`);
}

function dataForVersion(version) {
	const alias = versionAliases[version] ?? version;
	const data = mcDataForVersion(alias);
	if (!data) {
		throw new Error(`${version}: minecraft-data has no protocol data directory`);
	}
	if (protocolOverrides[version] && data.version.minecraftVersion !== version) {
		return {
			...data,
			version: {
				...data.version,
				minecraftVersion: version,
				version: protocolOverrides[version],
			},
		};
	}
	return data;
}

function protocolForVersion(version) {
	if (protocolOverrides[version]) {
		return protocolOverrides[version];
	}
	const direct = mcDataForVersion.versionsByMinecraftVersion?.pc?.[version];
	if (direct?.version) {
		return direct.version;
	}
	return dataForVersion(version).version.version;
}

function stoneStateFor(version) {
	const data = dataForVersion(version);
  if (!data?.blocksByName?.stone) {
    throw new Error(`${version}: minecraft-data has no stone block`);
  }
  const stone = data.blocksByName.stone;
  if (protocolForVersion(version) <= 340) {
    return stone.id << 4;
  }
  if (typeof stone.defaultState === "number") {
    return stone.defaultState;
  }
  throw new Error(`${version}: minecraft-data stone.defaultState missing`);
}

function firstBlockState(version, chunkData) {
  if (version.startsWith("1.8.")) {
    return chunkData.readUInt16BE(0);
  }
  return firstPalettedBlockState(chunkData, protocolForVersion(version) >= 477);
}

function firstPalettedBlockState(chunkData, hasNonAirCount) {
  const reader = new BufferReader(chunkData);
  if (hasNonAirCount) {
    reader.u16();
  }
  const bitsPerBlock = reader.u8();
  const paletteLen = reader.varint();
  const palette = [];
  for (let i = 0; i < paletteLen; i++) {
    palette.push(reader.varint());
  }
  const dataLen = reader.varint();
  if (dataLen <= 0) {
    throw new Error(`section has empty block state data array`);
  }
  const firstLong = reader.i64();
  const mask = (1n << BigInt(bitsPerBlock)) - 1n;
  const paletteIndex = Number(firstLong & mask);
  if (paletteIndex >= palette.length) {
    throw new Error(`palette index ${paletteIndex} outside palette ${JSON.stringify(palette)}`);
  }
  return palette[paletteIndex];
}

class BufferReader {
  constructor(buffer) {
    this.buffer = buffer;
    this.offset = 0;
  }

  u8() {
    this.need(1);
    return this.buffer[this.offset++];
  }

	u16() {
		this.need(2);
		const value = this.buffer.readUInt16BE(this.offset);
		this.offset += 2;
		return value;
	}

	i32() {
		this.need(4);
		const value = this.buffer.readInt32BE(this.offset);
		this.offset += 4;
		return value;
	}

	i64() {
		this.need(8);
		const value = BigInt.asUintN(64, this.buffer.readBigInt64BE(this.offset));
		this.offset += 8;
		return value;
	}

	bytes(count) {
		this.need(count);
		const value = this.buffer.subarray(this.offset, this.offset + count);
		this.offset += count;
		return value;
	}

  varint() {
    let value = 0;
    let shift = 0;
    for (let i = 0; i < 5; i++) {
      const byte = this.u8();
      value |= (byte & 0x7f) << shift;
      if ((byte & 0x80) === 0) {
        return value;
      }
      shift += 7;
    }
    throw new Error("varint too long");
  }

  need(count) {
    if (this.offset + count > this.buffer.length) {
      throw new Error(`buffer underflow at ${this.offset}, need ${count}, length ${this.buffer.length}`);
    }
  }
}

function goServerSource(port) {
  return `package main

import (
  "context"
  "fmt"
  "log"
  "net"

  "github.com/RoselleMC/authman/limbo"
  limbo "github.com/RoselleMC/authman/limbo/protocol/limbo"
)

func main() {
  worlds := limbgo.StaticWorldProvider{
    "legacy": makeWorld("legacy", 0, 256, 0),
    "modern": makeWorld("modern", -64, 384, -4),
  }
  srv, err := limbgo.NewServer(limbgo.Config{
    Addr: "127.0.0.1:${port}",
    ProtocolRouter: limbo.Router{Description: "limbgo js smoke"},
    Worlds: worlds,
    SpawnResolver: limbgo.SpawnResolverFunc(func(_ context.Context, player limbgo.Player) (limbgo.SpawnTarget, error) {
      world := "modern"
      if player.ProtocolVersion < 757 {
        world = "legacy"
      }
      return limbgo.SpawnTarget{
        World: world,
        Position: limbgo.Vec3{X: 0, Y: 65, Z: 0},
        GameMode: limbgo.GameModeAdventure,
      }, nil
    }),
  })
  if err != nil {
    log.Fatal(err)
  }
  ln, err := net.Listen("tcp", "127.0.0.1:${port}")
  if err != nil {
    log.Fatal(err)
  }
  fmt.Println("LISTEN ${port}")
  if err := srv.Serve(context.Background(), ln); err != nil {
    log.Fatal(err)
  }
}

func makeWorld(id string, minY int32, height int32, sectionY int32) *limbgo.MemoryWorld {
  blocks := make([]uint32, 16*16*16)
  for i := range blocks {
    blocks[i] = 1
  }
  return &limbgo.MemoryWorld{
    WorldID: id,
    WorldDimension: limbgo.Dimension{
      Name: "minecraft:overworld",
      MinY: minY,
      Height: height,
      Natural: true,
      HasSkylight: true,
      CoordinateScale: 1,
    },
    Palette: []limbgo.BlockState{
      {Name: "minecraft:air"},
      {Name: "minecraft:stone"},
    },
    Chunks: map[limbgo.ChunkPos]limbgo.Chunk{
      {X: 0, Z: 0}: {
        X: 0,
        Z: 0,
        MinY: minY,
        Sections: []limbgo.ChunkSection{
          {Y: sectionY, BlockStateIDs: blocks},
        },
      },
    },
  }
}
`;
}

async function main() {
  const port = await reservePort();
  const serverDir = fs.mkdtempSync(path.join(os.tmpdir(), "limbgo-js-smoke-server-"));
  fs.writeFileSync(
    path.join(serverDir, "go.mod"),
    `module limbgo-js-smoke

go 1.24.2

require github.com/RoselleMC/authman v0.0.0

replace github.com/RoselleMC/authman => ${repoRoot}
`,
  );
	fs.writeFileSync(path.join(serverDir, "main.go"), goServerSource(port));

	const serverPath = path.join(serverDir, "limbgo-js-smoke-server");
	execFileSync("go", ["mod", "tidy"], {
		cwd: serverDir,
		stdio: "inherit",
	});
	execFileSync("go", ["build", "-o", serverPath, "."], {
		cwd: serverDir,
		stdio: "inherit",
	});
  const server = spawn(serverPath, {
    stdio: ["ignore", "pipe", "pipe"],
  });

  try {
    await waitForServer(server, port);
    const results = [];
    for (const version of requestedVersions) {
      results.push(await checkVersion(port, version));
    }
    for (const result of results) {
      console.log(
        `${result.version}: protocol=${result.protocol} chunkData=${result.chunkDataLength} stoneState=${result.stoneState}`,
      );
    }
  } finally {
    await stopServer(server);
  }
}

await main();

async function stopServer(server) {
  if (server.exitCode !== null || server.signalCode !== null) {
    return;
  }
  const exited = new Promise((resolve) => server.once("exit", resolve));
  server.kill("SIGTERM");
  await Promise.race([
    exited,
    new Promise((resolve) => {
      setTimeout(() => {
        server.kill("SIGKILL");
        resolve();
      }, 3000);
    }),
  ]);
}
