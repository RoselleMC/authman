package com.iroselle.authman.bridge.protocol

data class BridgeSender(
    val kind: String = "",
    val name: String = "",
    val uniqueId: String = "",
)

data class BridgeRequest(
    val version: Int = PROTOCOL_VERSION,
    val type: String = "",
    val requestId: String = "",
    val sender: BridgeSender = BridgeSender(),
    val arguments: List<String> = emptyList(),
)

data class BridgeMessage(
    val text: String = "",
    val tone: String = "info",
    val actionType: String = "",
    val actionValue: String = "",
)

data class BridgeResponse(
    val version: Int = PROTOCOL_VERSION,
    val type: String = "result",
    val requestId: String = "",
    val actorId: String = "",
    val success: Boolean = false,
    val available: Boolean = false,
    val messages: List<BridgeMessage> = emptyList(),
    val suggestions: List<String> = emptyList(),
    val error: String = "",
)

const val PROTOCOL_VERSION: Int = 1
const val CHANNEL_NAME: String = "authman:bridge"
const val MAX_PAYLOAD_BYTES: Int = 30 * 1024
const val MAX_ARGUMENTS: Int = 32
const val MAX_ARGUMENT_LENGTH: Int = 256

private val REQUEST_ID_PATTERN = Regex("^[A-Za-z0-9_-]{8,96}$")

fun validRequestId(value: String): Boolean = REQUEST_ID_PATTERN.matches(value)
