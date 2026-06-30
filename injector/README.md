# Authman Injector

Authman Injector is a Java agent for downstream Paper-compatible servers that
need to accept Authman Unicode protocol names. It is intentionally narrow: it
only rewrites the Minecraft/Paper username validation helpers that block
non-ASCII player names.

Authman's transfer-based flow targets Minecraft Java Edition 1.20.5 and newer,
because vanilla transfer packets were introduced in 1.20.5.

Run it with a Paper/Canvas/Folia server:

```sh
java -javaagent:authman-injector.jar -jar paper.jar nogui
```

Optional agent arguments:

```sh
java -javaagent:authman-injector.jar=debug=true -jar paper.jar nogui
```

The default policy allows 1 to 16 Unicode letters or numbers plus `_`, and
rejects whitespace, control characters, emoji, and other symbols.

The current compatibility hooks cover Paper-compatible 1.20.6, 1.21.1, and
1.21.11 test servers. They also include the older `Player.isValidUsername`
helper used by nearby Paper lines.
