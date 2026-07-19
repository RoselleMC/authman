package com.iroselle.authman.bootstrap

import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.proxy.InboundConnection
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.util.GameProfile
import org.junit.jupiter.api.Test
import java.lang.reflect.Proxy
import java.net.InetSocketAddress
import java.util.UUID
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BootstrapGateTest {
    @Test
    fun `profile rename does not lose the admission attempt`() {
        val address = InetSocketAddress("192.0.2.10", 43123)
        val gate = BootstrapGate({ 7L }, { 1L })
        val original = GameProfile(UUID.randomUUID(), "YinRed", emptyList())
        val profileRequest = GameProfileRequestEvent(connection(address), original, false)

        gate.onGameProfileRequest(profileRequest)
        profileRequest.gameProfile = original.withName("Yin")

        val login = LoginEvent(player(address, "Yin"))
        gate.onLogin(login)

        assertTrue(login.result.isAllowed)
    }

    @Test
    fun `runtime swap during login still denies the attempt`() {
        val address = InetSocketAddress("192.0.2.10", 43124)
        var generation = 7L
        val gate = BootstrapGate({ generation }, { 1L })
        val original = GameProfile(UUID.randomUUID(), "Player", emptyList())

        gate.onGameProfileRequest(GameProfileRequestEvent(connection(address), original, false))
        generation = 8L

        val login = LoginEvent(player(address, "Player"))
        gate.onLogin(login)

        assertFalse(login.result.isAllowed)
    }

    @Test
    fun `expired admission attempt is denied even without another connection`() {
        val address = InetSocketAddress("192.0.2.10", 43125)
        var now = 1L
        val gate = BootstrapGate({ 7L }, { now })
        val original = GameProfile(UUID.randomUUID(), "Player", emptyList())

        gate.onGameProfileRequest(GameProfileRequestEvent(connection(address), original, false))
        now += 31L * 1_000_000_000L

        val login = LoginEvent(player(address, "Player"))
        gate.onLogin(login)

        assertFalse(login.result.isAllowed)
    }

    private fun connection(address: InetSocketAddress): InboundConnection =
        proxy(InboundConnection::class.java, address, "Player")

    private fun player(address: InetSocketAddress, username: String): Player =
        proxy(Player::class.java, address, username)

    private fun <T> proxy(type: Class<T>, address: InetSocketAddress, username: String): T {
        val instance = Proxy.newProxyInstance(type.classLoader, arrayOf(type)) { proxy, method, args ->
            when (method.name) {
                "getRemoteAddress" -> address
                "getUsername" -> username
                "isActive" -> true
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                "toString" -> "$username@$address"
                else -> defaultValue(method.returnType)
            }
        }
        return type.cast(instance)
    }

    private fun defaultValue(type: Class<*>): Any? = when (type) {
        java.lang.Boolean.TYPE -> false
        java.lang.Byte.TYPE -> 0.toByte()
        java.lang.Short.TYPE -> 0.toShort()
        java.lang.Integer.TYPE -> 0
        java.lang.Long.TYPE -> 0L
        java.lang.Float.TYPE -> 0f
        java.lang.Double.TYPE -> 0.0
        java.lang.Character.TYPE -> '\u0000'
        else -> null
    }
}
