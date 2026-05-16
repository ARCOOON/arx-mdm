package com.arx.mdm.network

import android.content.Context
import android.os.SystemClock
import com.arx.mdm.ui.PingResult
import okhttp3.Request
import java.util.concurrent.TimeUnit

/**
 * Lightweight mTLS reachability check: GET on [TelemetryService] path completes TLS handshake
 * (405 Method Not Allowed is expected and treated as success).
 */
object ArxServerPing {

    fun ping(context: Context): PingResult {
        val t0 = SystemClock.elapsedRealtime()
        val secure = ArxSecureState(context)
        val base = secure.getServerUrl()?.trim()?.trimEnd('/')
        if (base.isNullOrEmpty()) {
            return PingResult(0, elapsed(t0), "no server URL")
        }
        if (!secure.isMtlsEnrolled()) {
            return PingResult(0, elapsed(t0), "not enrolled")
        }
        return try {
            val client = ArxMtlsRetrofit.mtlsOkHttpClient(secure).newBuilder()
                .readTimeout(30, TimeUnit.SECONDS)
                .callTimeout(30, TimeUnit.SECONDS)
                .build()
            val req = Request.Builder()
                .url("$base/v1/telemetry")
                .get()
                .build()
            client.newCall(req).execute().use { resp ->
                PingResult(resp.code, elapsed(t0), null)
            }
        } catch (e: Exception) {
            PingResult(0, elapsed(t0), e.message ?: "request failed")
        }
    }

    private fun elapsed(start: Long): Long = (SystemClock.elapsedRealtime() - start).coerceAtLeast(0L)
}
