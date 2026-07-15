#!/usr/bin/env node

import { createRequire } from "node:module";
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const defaultVersions = ["1.21.5", "1.21.6", "1.21.8", "1.21.9", "1.21.11", "26.1.2"];
const versionAliases = {
  "26.1": "1.21.11",
  "26.1.1": "1.21.11",
  "26.1.2": "1.21.11",
};
const requestedVersions = process.argv.length > 2 ? process.argv.slice(2) : defaultVersions;

const require = createRequire(import.meta.url);
let mcDataForVersion;
try {
  mcDataForVersion = require("minecraft-data");
} catch {
  const depsDir = fs.mkdtempSync(path.join(os.tmpdir(), "limbgo-js-dialog-deps-"));
  fs.writeFileSync(path.join(depsDir, "package.json"), JSON.stringify({ private: true }));
  execFileSync("npm", ["install", "--silent", "minecraft-data@latest"], {
    cwd: depsDir,
    stdio: "inherit",
  });
  mcDataForVersion = createRequire(path.join(depsDir, "package.json"))("minecraft-data");
}

async function reservePort() {
  const srv = net.createServer();
  await new Promise((resolve, reject) => {
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", resolve);
  });
  const { port } = srv.address();
  await new Promise((resolve) => srv.close(resolve));
  return port;
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

async function checkVersion(port, version) {
  const data = dataForVersion(version);
  const protocol = protocolForVersion(version);
  const showDialog = optionalPacketId(data, "play", "toClient", "show_dialog");
  if (showDialog == null) {
    return { version, protocol, skipped: "show_dialog unavailable" };
  }

  return new Promise((resolve, reject) => {
    const ids = {
      loginStart: packetId(data, "login", "toServer", "login_start"),
      loginSuccess: packetId(data, "login", "toClient", "success"),
      loginAcknowledged: packetId(data, "login", "toServer", "login_acknowledged"),
      finishConfigurationClient: packetId(data, "configuration", "toClient", "finish_configuration"),
      finishConfigurationServer: packetId(data, "configuration", "toServer", "finish_configuration"),
      showDialog,
      clearDialog: packetId(data, "play", "toClient", "clear_dialog"),
      customClickAction: packetId(data, "play", "toServer", "custom_click_action"),
      storeCookie: packetId(data, "play", "toClient", "store_cookie"),
      transfer: packetId(data, "play", "toClient", "transfer"),
    };
    const socket = net.createConnection({ host: "127.0.0.1", port });
    let buffer = Buffer.alloc(0);
    let state = "login";
    let sawShowDialog = false;
    let sawClearDialog = false;
    let sawStoreCookie = false;
    let sentClick = false;
    let done = false;
    const timeout = setTimeout(() => {
      finish(new Error(`${version}: timed out; state=${state} show=${sawShowDialog} clear=${sawClearDialog} click=${sentClick}`));
    }, 20000);

    function finish(err, value) {
      if (done) return;
      done = true;
      clearTimeout(timeout);
      socket.destroy();
      if (err) reject(err);
      else resolve(value);
    }

    socket.on("connect", () => {
      socket.write(packet(0, handshakePayload(protocol, "127.0.0.1", port)));
      socket.write(packet(ids.loginStart, loginStartPayload(data, `Dlg${version.replaceAll(".", "")}`)));
    });
    socket.on("data", (chunk) => {
      buffer = Buffer.concat([buffer, chunk]);
      try {
        for (;;) {
          const next = readFrame(buffer);
          if (!next) return;
          buffer = buffer.subarray(next.frameEnd);
          handlePacket(next.id, next.payload);
        }
      } catch (err) {
        finish(err);
      }
    });
    socket.on("error", (err) => finish(err));
    socket.on("close", () => {
      if (!done) finish(new Error(`${version}: connection closed before dialog flow completed`));
    });

    function handlePacket(id, payload) {
      if (state === "login") {
        if (id !== ids.loginSuccess) return;
        socket.write(packet(ids.loginAcknowledged));
        state = "configuration";
        return;
      }
      if (state === "configuration") {
        if (id !== ids.finishConfigurationClient) return;
        socket.write(packet(ids.finishConfigurationServer));
        state = "play";
        return;
      }
      if (id === ids.showDialog) {
        assertInlineDialogPayload(version, payload);
        sawShowDialog = true;
        return;
      }
      if (id === ids.clearDialog) {
        if (!sawShowDialog) throw new Error(`${version}: clear_dialog arrived before show_dialog`);
        sawClearDialog = true;
        socket.write(packet(ids.customClickAction, Buffer.concat([writeString("limbgo:dialog-smoke"), Buffer.from([0])])));
        sentClick = true;
        return;
      }
      if (id === ids.storeCookie && sentClick) {
        sawStoreCookie = true;
        return;
      }
      if (id === ids.transfer && sawStoreCookie) {
        finish(null, {
          version,
          protocol,
          showDialog: ids.showDialog,
          clearDialog: ids.clearDialog,
          customClickAction: ids.customClickAction,
          storeCookie: ids.storeCookie,
          transfer: ids.transfer,
        });
      }
    }
  });
}

function assertInlineDialogPayload(version, payload) {
  const holder = readVarIntFrom(payload, 0);
  if (!holder) throw new Error(`${version}: show_dialog missing registry holder`);
  if (holder.value !== 0) throw new Error(`${version}: show_dialog holder=${holder.value}, want inline holder 0`);
  if (payload.length <= holder.size) throw new Error(`${version}: show_dialog missing anonymous NBT`);
  const tag = payload[holder.size];
  if (tag !== 10) throw new Error(`${version}: show_dialog anonymous NBT tag=${tag}, want compound 10`);
}

function packetId(data, state, direction, name) {
  const id = optionalPacketId(data, state, direction, name);
  if (id == null) throw new Error(`${data.version.minecraftVersion}: missing ${state}.${direction}.${name}`);
  return id;
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

function optionalPacketId(data, state, direction, name) {
  const packetType = data.protocol?.[state]?.[direction]?.types?.packet;
  if (!packetType) return null;
  const nameField = packetType[1].find((field) => field.name === "name");
  const mappings = nameField?.type?.[1]?.mappings;
  if (!mappings) return null;
  for (const [rawID, packetName] of Object.entries(mappings)) {
    if (packetName === name) return Number.parseInt(rawID, 16);
  }
  return null;
}

function dataForVersion(version) {
  const alias = versionAliases[version] ?? version;
  const data = mcDataForVersion(alias);
  if (!data) throw new Error(`${version}: minecraft-data has no protocol data directory`);
  return data;
}

function protocolForVersion(version) {
  const direct = mcDataForVersion.versionsByMinecraftVersion?.pc?.[version];
  if (direct?.version) return direct.version;
  return dataForVersion(version).version.version;
}

function packet(id, payload = Buffer.alloc(0)) {
  const body = Buffer.concat([writeVarInt(id), payload]);
  return Buffer.concat([writeVarInt(body.length), body]);
}

function handshakePayload(protocol, host, port) {
  return Buffer.concat([writeVarInt(protocol), writeString(host), writeU16(port), writeVarInt(2)]);
}

function readFrame(buffer) {
  const length = readVarIntFrom(buffer, 0);
  if (!length) return null;
  const frameStart = length.size;
  const frameEnd = frameStart + length.value;
  if (buffer.length < frameEnd) return null;
  const id = readVarIntFrom(buffer, frameStart);
  if (!id) return null;
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
    if (offset + i >= buffer.length) return null;
    const byte = buffer[offset + i];
    value |= (byte & 0x7f) << shift;
    if ((byte & 0x80) === 0) return { value, size: i + 1 };
    shift += 7;
  }
  throw new Error("varint too long");
}

function goServerSource(port) {
  return `package main

import (
  "context"
  "fmt"
  "log"
  "net"

  "github.com/RoselleMC/authman/limbo"
  "github.com/RoselleMC/authman/limbo/dialog"
  limbo "github.com/RoselleMC/authman/limbo/protocol/limbo"
  "go.minekube.com/common/minecraft/component"
)

func main() {
  world := makeWorld()
  srv, err := limbgo.NewServer(limbgo.Config{
    Addr: "127.0.0.1:${port}",
    ProtocolRouter: limbo.Router{Description: "limbgo dialog js smoke"},
    Worlds: limbgo.StaticWorldProvider{"spawn": world},
    SpawnResolver: limbgo.StaticSpawn(limbgo.SpawnTarget{
      World: "spawn",
      Position: limbgo.Vec3{X: 0, Y: 65, Z: 0},
      GameMode: limbgo.GameModeAdventure,
    }),
    Events: limbgo.PlayerEventHandlerFuncs{
      Join: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.JoinEvent) error {
        if event.Protocol < 771 {
          return session.SendMessage(ctx, &component.Text{Content: "dialog unsupported"})
        }
        if err := session.ShowDialog(ctx, dialog.Notice(dialog.Common{
          Title: &component.Text{
            Content: "Dialog",
            Extra: []component.Component{&component.Text{Content: " rich"}},
          },
          Body: []dialog.Raw{
            dialog.PlainMessage(&component.Text{Content: "Body rich text"}, 220),
          },
          Inputs: []dialog.Raw{
            dialog.TextInput("name", &component.Text{Content: "Name"}, dialog.TextInputOptions{
              Initial: "Steve",
              MaxLength: 32,
            }),
          },
          Pause: dialog.Bool(false),
          AfterAction: dialog.AfterActionWaitForResponse,
        }, dialog.ActionButton{
          Label: &component.Text{Content: "Submit"},
          Tooltip: &component.Text{Content: "Tooltip rich text"},
          Action: dialog.DynamicCustom("limbgo:dialog-smoke", dialog.Raw{"source": "js-smoke"}),
        })); err != nil {
          return err
        }
        return session.ClearDialog(ctx)
      },
      DialogClick: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.DialogClickEvent) error {
        if err := session.StoreCookie(ctx, "authman:transfer", []byte(event.ID)); err != nil {
          return err
        }
        return session.Transfer(ctx, "velocity.internal", 25566)
      },
    },
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

func makeWorld() *limbgo.MemoryWorld {
  blocks := make([]uint32, 16*16*16)
  for i := range blocks {
    blocks[i] = 1
  }
  return &limbgo.MemoryWorld{
    WorldID: "spawn",
    WorldDimension: limbgo.Dimension{
      Name: "minecraft:overworld",
      MinY: -64,
      Height: 384,
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
        MinY: -64,
        Sections: []limbgo.ChunkSection{
          {Y: -4, BlockStateIDs: blocks},
        },
      },
    },
  }
}
`;
}

async function main() {
  const port = await reservePort();
  const serverDir = fs.mkdtempSync(path.join(os.tmpdir(), "limbgo-js-dialog-server-"));
  fs.writeFileSync(
    path.join(serverDir, "go.mod"),
    `module limbgo-js-dialog

go 1.24.2

require github.com/RoselleMC/authman v0.0.0

replace github.com/RoselleMC/authman => ${repoRoot}
`,
  );
  fs.writeFileSync(path.join(serverDir, "main.go"), goServerSource(port));

  const serverPath = path.join(serverDir, "limbgo-js-dialog-server");
  execFileSync("go", ["mod", "tidy"], { cwd: serverDir, stdio: "inherit" });
  execFileSync("go", ["build", "-o", serverPath, "."], { cwd: serverDir, stdio: "inherit" });

  const proc = spawn(serverPath, [], { cwd: serverDir, stdio: ["ignore", "pipe", "pipe"] });
  try {
    await waitForServer(proc, port);
    for (const version of requestedVersions) {
      const result = await checkVersion(port, version);
      if (result.skipped) {
        console.log(`${result.version}: protocol=${result.protocol} skipped=${result.skipped}`);
      } else {
        console.log(
          `${result.version}: protocol=${result.protocol} show_dialog=0x${result.showDialog.toString(16)} ` +
            `clear_dialog=0x${result.clearDialog.toString(16)} custom_click_action=0x${result.customClickAction.toString(16)} ` +
            `store_cookie=0x${result.storeCookie.toString(16)} transfer=0x${result.transfer.toString(16)}`,
        );
      }
    }
  } finally {
    proc.kill("SIGTERM");
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
