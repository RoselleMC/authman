package com.iroselle.authman.bootstrap

import com.iroselle.authman.spi.AUTHMAN_RUNTIME_API_VERSION
import com.iroselle.authman.spi.AUTHMAN_RUNTIME_CONTRACT
import org.junit.jupiter.api.Test
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class RuntimeCompatibilityTest {
    @Test
    fun `frozen runtime contract and migration runtime are accepted`() {
        RuntimeCompatibility.requireCompatible(AUTHMAN_RUNTIME_API_VERSION, AUTHMAN_RUNTIME_CONTRACT)
        RuntimeCompatibility.requireCompatible(RuntimeCompatibility.MIGRATION_API_VERSION, null)
    }

    @Test
    fun `future API bumps and mismatched contracts are rejected`() {
        assertFailsWith<IllegalArgumentException> {
            RuntimeCompatibility.requireCompatible(AUTHMAN_RUNTIME_API_VERSION + 1, AUTHMAN_RUNTIME_CONTRACT)
        }
        assertFailsWith<IllegalArgumentException> {
            RuntimeCompatibility.requireCompatible(AUTHMAN_RUNTIME_API_VERSION, "another.contract")
        }
    }

    @Test
    fun `bootstrap command ownership follows the runtime contract`() {
        assertTrue(RuntimeCompatibility.bootstrapOwnsCommand(null))
        assertTrue(RuntimeCompatibility.bootstrapOwnsCommand(AUTHMAN_RUNTIME_API_VERSION))
        assertFalse(RuntimeCompatibility.bootstrapOwnsCommand(RuntimeCompatibility.MIGRATION_API_VERSION))
    }
}
