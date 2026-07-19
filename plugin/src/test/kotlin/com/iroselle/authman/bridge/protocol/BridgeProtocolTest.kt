package com.iroselle.authman.bridge.protocol

import com.google.gson.Gson
import org.junit.jupiter.api.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BridgeProtocolTest {
    @Test
    fun `request round trips without platform classes`() {
        val request = BridgeRequest(
            type = "execute",
            requestId = "request_12345678",
            sender = BridgeSender("player", "TestPlayer", "00000000-0000-0000-0000-000000000001"),
            arguments = listOf("status"),
        )
        val decoded = Gson().fromJson(Gson().toJson(request), BridgeRequest::class.java)

        assertEquals(request, decoded)
        assertEquals(PROTOCOL_VERSION, decoded.version)
    }

    @Test
    fun `request ids are bounded`() {
        assertTrue(validRequestId("request_12345678"))
        assertFalse(validRequestId("short"))
        assertFalse(validRequestId("request with spaces"))
        assertFalse(validRequestId("a".repeat(97)))
    }
}
