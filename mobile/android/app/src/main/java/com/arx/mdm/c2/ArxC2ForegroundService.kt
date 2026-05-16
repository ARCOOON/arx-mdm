package com.arx.mdm.c2

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.core.app.NotificationCompat
import com.arx.mdm.PolicyManager
import com.arx.mdm.R
import com.arx.mdm.network.ArxMtlsRetrofit
import com.arx.mdm.network.ArxSecureState
import com.google.gson.JsonParser
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.resume
import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.io.File
import java.io.IOException
import kotlin.math.min
import okhttp3.OkHttpClient

/**
 * Holds an authenticated WebSocket toward the enrollment server so catalog installs may be streamed.
 */
class ArxC2ForegroundService : Service() {

    private val supervisorJob = SupervisorJob()
    private val uiScope = CoroutineScope(supervisorJob + Dispatchers.Main.immediate)
    private var loopJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        maybeCreateNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForegroundWithNotification()
        loopJob?.cancel()
        loopJob = uiScope.launch { runReconnectLoop() }
        return START_STICKY
    }

    override fun onDestroy() {
        loopJob?.cancel()
        ArxC2Session.attach(null)
        supervisorJob.cancel()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?) = null

    private fun startForegroundWithNotification() {
        val notification = NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.arx_c2_notification_title))
            .setContentText(getString(R.string.arx_c2_notification_body))
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setOngoing(true)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIFY_ID,
                notification,
                android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
            )
        } else {
            @Suppress("DEPRECATION")
            startForeground(NOTIFY_ID, notification)
        }
    }

    private suspend fun runReconnectLoop() {
        val state = ArxSecureState(applicationContext)
        var backoffMs = INITIAL_BACKOFF_MS
        while (uiScope.isActive) {
            if (!state.isMtlsEnrolled()) {
                Log.i(TAG, "Stopping C2 service (missing enrollment)")
                stopSelf()
                return
            }
            val server = state.getServerUrl()?.trim().orEmpty()
            if (server.isEmpty()) {
                delay(backoffMs)
                continue
            }
            try {
                val wsUrl = toWebSocketEndpoint(server)
                val client = ArxMtlsRetrofit.mtlsOkHttpClient(state)
                suspendCancellableCoroutine<Unit> { continuation ->
                    val listener = object : WebSocketListener() {
                        override fun onOpen(webSocket: WebSocket, response: Response) {
                            Log.i(TAG, "C2 websocket open")
                            ArxC2Session.attach(webSocket)
                        }

                        override fun onMessage(webSocket: WebSocket, text: String) {
                            dispatchInstallPayload(client, state, text)
                        }

                        override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                            webSocket.close(code, reason)
                        }

                        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                            ArxC2Session.attach(null)
                            Log.i(TAG, "C2 websocket closed code=$code")
                            if (continuation.isActive) {
                                continuation.resume(Unit)
                            }
                        }

                        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                            ArxC2Session.attach(null)
                            Log.w(TAG, "C2 websocket failure", t)
                            if (continuation.isActive) {
                                continuation.resumeWith(Result.failure(t))
                            }
                        }
                    }

                    val request = Request.Builder().url(wsUrl).build()
                    val socket = client.newWebSocket(request, listener)
                    continuation.invokeOnCancellation { socket.cancel() }
                }
                backoffMs = INITIAL_BACKOFF_MS
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.w(TAG, "Reconnecting after transient error (${e.javaClass.simpleName})", e)
                backoffMs = min(MAX_BACKOFF_MS, backoffMs * 2)
            }
            delay(backoffMs.coerceAtMost(MAX_BACKOFF_MS))
        }
    }

    private fun dispatchInstallPayload(client: OkHttpClient, state: ArxSecureState, raw: String) {
        uiScope.launch(Dispatchers.IO) {
            try {
                val root = JsonParser.parseString(raw).asJsonObject
                val action = root.get("action")?.asString?.trim()?.lowercase().orEmpty()
                if (action != INSTALL_ACTION) {
                    return@launch
                }
                val appId = root.get("app_id")?.asString?.trim().orEmpty()
                val artifact = root.get("artifact_path")?.asString?.trim().orEmpty()
                if (appId.isEmpty() || artifact.isEmpty()) {
                    ArxC2Session.reportInstallResult(
                        appId,
                        false,
                        -21,
                        "missing catalog identifiers",
                    )
                    return@launch
                }
                val apkBytes = fetchArtifactPayload(client, state.getServerUrl().orEmpty(), artifact)
                val temp =
                    File.createTempFile("arx-catalog-", ".apk", cacheDir)
                try {
                    temp.outputStream().use { it.write(apkBytes) }
                    PolicyManager(applicationContext).installApkSilently(temp, appId)
                } finally {
                    temp.delete()
                }
            } catch (e: IOException) {
                Log.e(TAG, "download failed for install directive", e)
                trySignalFailure(raw, -31, e.message ?: "download failed")
            } catch (e: Exception) {
                Log.e(TAG, "unexpected install directive error", e)
            }
        }
    }

    private fun trySignalFailure(raw: String, exitCode: Int, message: String) {
        runCatching {
            val root = JsonParser.parseString(raw).asJsonObject
            val appId = root.get("app_id")?.asString.orEmpty()
            ArxC2Session.reportInstallResult(appId, false, exitCode, message)
        }
    }

    @Throws(IOException::class)
    private fun fetchArtifactPayload(
        client: OkHttpClient,
        serverBaseRaw: String,
        artifact: String,
    ): ByteArray {
        val trimmed = artifact.trim()
        val url =
            when {
                trimmed.startsWith("https://", true) || trimmed.startsWith("http://", true) ->
                    trimmed

                else -> {
                    val base = serverBaseRaw.trim().trimEnd('/')
                    val path = if (trimmed.startsWith("/")) trimmed else "/$trimmed"
                    "$base$path"
                }
            }
        val request = Request.Builder().url(url).build()
        return client.newCall(request).execute().use { resp ->
            if (!resp.isSuccessful) {
                throw IOException("artifact fetch failed (${resp.code})")
            }
            resp.body?.bytes() ?: throw IOException("artifact body missing")
        }
    }

    private fun maybeCreateNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val mgr = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.arx_c2_channel_name),
            NotificationManager.IMPORTANCE_LOW,
        ).apply { description = "Maintains authenticated command/control connectivity" }
        mgr.createNotificationChannel(channel)
    }

    private fun toWebSocketEndpoint(serverHttps: String): String {
        val trimmed = serverHttps.trim().trimEnd('/')
        return when {
            trimmed.startsWith("https://", true) ->
                "wss://${trimmed.substring("https://".length)}/v1/ws"

            trimmed.startsWith("http://", true) ->
                "ws://${trimmed.substring("http://".length)}/v1/ws"

            else -> error("server URL scheme not supported")
        }
    }

    companion object {
        private const val TAG = "ArxC2ForegroundService"
        private const val NOTIFY_ID = 26001
        private const val CHANNEL_ID = "arx_mdm_agent_c2"
        private const val INITIAL_BACKOFF_MS = 3000L
        private const val MAX_BACKOFF_MS = 60_000L
        private const val INSTALL_ACTION = "install_app"

        fun enqueue(context: Context) {
            val secure = ArxSecureState(context.applicationContext)
            if (!secure.isMtlsEnrolled()) return
            val intent = Intent(context, ArxC2ForegroundService::class.java)
            ContextCompat.startForegroundService(context, intent)
        }
    }
}
