package com.iroselle.authman.command

import org.junit.jupiter.api.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class AuthmanCommandTest {
    @Test
    fun `duration parser keeps month and minute distinct`() {
        assertEquals(60, AuthmanCommand.parseDurationSeconds("1min"))
        assertEquals(30L * 24L * 60L * 60L, AuthmanCommand.parseDurationSeconds("1m"))
        assertEquals(365L * 24L * 60L * 60L, AuthmanCommand.parseDurationSeconds("1y"))
    }

    @Test
    fun `duration parser rejects overflow and zero`() {
        assertNull(AuthmanCommand.parseDurationSeconds("0s"))
        assertNull(AuthmanCommand.parseDurationSeconds("9999999999999999999y"))
    }
}
