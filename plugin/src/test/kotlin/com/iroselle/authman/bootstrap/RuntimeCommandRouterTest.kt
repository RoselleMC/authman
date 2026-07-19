package com.iroselle.authman.bootstrap

import com.iroselle.authman.spi.AuthmanCommandActor
import com.iroselle.authman.spi.AuthmanCommandActorKind
import com.iroselle.authman.spi.AuthmanCommandDescriptor
import com.iroselle.authman.spi.AuthmanCommandMessage
import com.iroselle.authman.spi.AuthmanCommandProvider
import com.iroselle.authman.spi.RuntimeControl
import org.junit.jupiter.api.Test
import org.slf4j.LoggerFactory
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class RuntimeCommandRouterTest {
    @Test
    fun `only reload is visible until Core and runtime are ready`() {
        val control = FakeControl()
        val router = RuntimeCommandRouter(LoggerFactory.getLogger(javaClass), control)
        val actor = FakeActor(setOf(RuntimeCommandRouter.PERMISSION_ADMIN, "authman.command.status"))

        assertEquals(listOf("reload"), router.suggest(actor, listOf("")))
        router.setCoreConnected(true)
        router.resumeRuntime()
        assertEquals(listOf("reload"), router.suggest(actor, listOf("")))

        val registration = router.install(FakeProvider())
        assertEquals(listOf("reload", "status"), router.suggest(actor, listOf("")))
        assertTrue(router.isOperational())

        router.suspendRuntime()
        assertEquals(listOf("reload"), router.suggest(actor, listOf("")))
        assertFalse(router.isOperational())

        registration.close()
    }

    @Test
    fun `runtime capabilities remain permission aware and are removed with registration`() {
        val router = RuntimeCommandRouter(LoggerFactory.getLogger(javaClass), FakeControl())
        val actor = FakeActor(setOf("authman.command.status"))
        val registration = router.install(FakeProvider())
        router.resumeRuntime()
        router.setCoreConnected(true)

        assertTrue(router.canUse(actor))
        assertEquals(listOf("status"), router.suggest(actor, listOf("")))
        assertTrue(router.execute(actor, listOf("status")))
        assertEquals("runtime status", actor.messages.single().text)

        registration.close()
        assertFalse(router.canUse(actor))
        assertTrue(router.suggest(actor, listOf("")).isEmpty())
    }

    @Test
    fun `reload remains bootstrap owned`() {
        val control = FakeControl(reloadResult = true)
        val router = RuntimeCommandRouter(LoggerFactory.getLogger(javaClass), control)
        val actor = FakeActor(setOf(RuntimeCommandRouter.PERMISSION_RELOAD))

        assertTrue(router.execute(actor, listOf("reload")))
        assertEquals(1, control.reloadCalls)
        assertTrue(actor.messages.single().text.contains("Core is available"))
    }

    private class FakeProvider : AuthmanCommandProvider {
        override fun descriptors(actor: AuthmanCommandActor): List<AuthmanCommandDescriptor> =
            if (actor.hasPermission("authman.command.status")) {
                listOf(AuthmanCommandDescriptor("status", summary = "Status", usage = "/authman status"))
            } else {
                emptyList()
            }

        override fun execute(actor: AuthmanCommandActor, arguments: List<String>): Boolean {
            if (arguments.firstOrNull() != "status") return false
            actor.send(AuthmanCommandMessage("runtime status"))
            return true
        }

        override fun suggest(actor: AuthmanCommandActor, arguments: List<String>): List<String> = emptyList()
    }

    private class FakeActor(
        private val permissions: Set<String>,
    ) : AuthmanCommandActor {
        override val kind = AuthmanCommandActorKind.PLAYER
        override val name = "TestPlayer"
        override val uniqueId = "00000000-0000-0000-0000-000000000001"
        val messages = mutableListOf<AuthmanCommandMessage>()

        override fun hasPermission(permission: String): Boolean = permission in permissions

        override fun send(message: AuthmanCommandMessage) {
            messages += message
        }
    }

    private class FakeControl(
        private val reloadResult: Boolean = false,
    ) : RuntimeControl {
        var reloadCalls = 0

        override fun reconnectNow(): Boolean = false

        override fun reloadBootstrapConfigAndReconnect(): Boolean {
            reloadCalls++
            return reloadResult
        }

        override fun isCoreAccessRevoked(): Boolean = false

        override fun lockCoreAccess(reason: String) = Unit
    }
}
