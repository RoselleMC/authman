package com.iroselle.authman.bridge.api

import org.bukkit.command.CommandSender
import java.util.concurrent.CompletableFuture

interface AuthmanBridge {
    val available: Boolean

    fun execute(sender: CommandSender, arguments: List<String>): CompletableFuture<AuthmanBridgeResult>
    fun suggest(sender: CommandSender, arguments: List<String>): CompletableFuture<List<String>>
}

data class AuthmanBridgeResult(
    val success: Boolean,
    val messages: List<AuthmanBridgeMessage>,
)

data class AuthmanBridgeMessage(
    val text: String,
    val tone: String,
    val actionType: String = "",
    val actionValue: String = "",
)
