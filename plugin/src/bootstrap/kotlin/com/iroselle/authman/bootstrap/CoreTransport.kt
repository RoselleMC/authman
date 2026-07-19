package com.iroselle.authman.bootstrap

import com.iroselle.authman.spi.RuntimeHttpResponse
import com.iroselle.authman.spi.RuntimeTransport
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.net.http.WebSocket
import java.nio.ByteBuffer
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CompletionStage

class CoreTransport(
    config: BootstrapConfig,
    private val instanceFingerprint: String,
) : RuntimeTransport, AutoCloseable {
    @Volatile
    private var config: BootstrapConfig = config

    @Volatile
    private var client: HttpClient = newClient(config)

    @Volatile
    private var activeWebSocket: WebSocket? = null

    @Volatile
    private var closed = false

    fun replaceConfig(next: BootstrapConfig) {
        closeEventStream()
        config = next
        client = newClient(next)
    }

    override fun post(path: String, body: String): RuntimeHttpResponse =
        request(path, body, includeInstance = true)

    fun heartbeat(body: String): RuntimeHttpResponse =
        request("/api/node/heartbeat", body, includeInstance = true)

    fun download(path: String, maximumBytes: Long): ByteArray {
        val current = config
        val request = HttpRequest.newBuilder()
            .uri(resolve(current.apiBase, path))
            .timeout(current.requestTimeout)
            .header("Authorization", "Bearer ${current.nodeToken}")
            .header("X-Authman-Instance", instanceFingerprint)
            .GET()
            .build()
        val response = client.send(request, HttpResponse.BodyHandlers.ofInputStream())
        return response.body().use { stream ->
            if (response.statusCode() !in 200..299) {
                throw IllegalStateException("runtime download failed with HTTP ${response.statusCode()}")
            }
            val body = stream.readNBytes(Math.toIntExact(maximumBytes + 1))
            require(body.isNotEmpty()) { "runtime download was empty" }
            require(body.size.toLong() <= maximumBytes) { "runtime download exceeded the size limit" }
            body
        }
    }

    fun connectEvents(onText: (String) -> String?): CompletableFuture<Void> {
        val done = CompletableFuture<Void>()
        val buffer = StringBuilder()
        val current = config
        client.newWebSocketBuilder()
            .header("Authorization", "Bearer ${current.nodeToken}")
            .header("X-Authman-Instance", instanceFingerprint)
            .buildAsync(resolveWebSocket(current.apiBase, "/api/node/events"), object : WebSocket.Listener {
                override fun onText(webSocket: WebSocket, data: CharSequence, last: Boolean): CompletionStage<*> {
                    buffer.append(data)
                    if (last) {
                        val message = buffer.toString()
                        buffer.setLength(0)
                        try {
                            onText(message)?.let { webSocket.sendText(it, true) }
                        } catch (ex: Exception) {
                            webSocket.abort()
                            done.completeExceptionally(ex)
                        }
                    }
                    webSocket.request(1)
                    return CompletableFuture.completedFuture(null)
                }

                override fun onPing(webSocket: WebSocket, message: ByteBuffer): CompletionStage<*> {
                    webSocket.request(1)
                    return webSocket.sendPong(message)
                }

                override fun onError(webSocket: WebSocket, error: Throwable) {
                    done.completeExceptionally(error)
                }

                override fun onClose(webSocket: WebSocket, statusCode: Int, reason: String): CompletionStage<*> {
                    done.complete(null)
                    return CompletableFuture.completedFuture(null)
                }
            })
            .whenComplete { webSocket, error ->
                if (error != null) {
                    done.completeExceptionally(error)
                } else if (closed || config !== current) {
                    webSocket.abort()
                    done.complete(null)
                } else {
                    activeWebSocket = webSocket
                    done.whenComplete { _, _ ->
                        if (activeWebSocket === webSocket) {
                            activeWebSocket = null
                        }
                    }
                    webSocket.request(1)
                }
            }
        return done
    }

    override fun close() {
        closed = true
        closeEventStream()
    }

    private fun closeEventStream() {
        val webSocket = activeWebSocket
        activeWebSocket = null
        webSocket?.abort()
    }

    private fun request(path: String, body: String, includeInstance: Boolean): RuntimeHttpResponse {
        val current = config
        val builder = HttpRequest.newBuilder()
            .uri(resolve(current.apiBase, path))
            .timeout(current.requestTimeout)
            .header("Authorization", "Bearer ${current.nodeToken}")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
        if (includeInstance) {
            builder.header("X-Authman-Instance", instanceFingerprint)
        }
        val response = client.send(builder.build(), HttpResponse.BodyHandlers.ofString())
        return RuntimeHttpResponse(response.statusCode(), response.body())
    }

    private fun newClient(config: BootstrapConfig): HttpClient = HttpClient.newBuilder()
        .connectTimeout(config.requestTimeout)
        .build()

    private fun resolve(base: URI, path: String): URI =
        URI.create("${base.toString().trimEnd('/')}/${path.trimStart('/')}")

    private fun resolveWebSocket(base: URI, path: String): URI {
        val uri = resolve(base, path)
        val scheme = when (uri.scheme.lowercase()) {
            "https" -> "wss"
            "http" -> "ws"
            "wss", "ws" -> uri.scheme
            else -> "ws"
        }
        return URI(scheme, uri.userInfo, uri.host, uri.port, uri.path, uri.query, uri.fragment)
    }
}
