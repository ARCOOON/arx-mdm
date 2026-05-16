package com.arx.mdm.c2

import android.util.Log
import com.google.gson.Gson
import okhttp3.WebSocket

/**
 * Lightweight holder for the active agent WebSocket so broadcast receivers can report install outcomes.
 */
object ArxC2Session {

    private val gson = Gson()

    @Volatile
    private var socket: WebSocket? = null

    fun attach(socket: WebSocket?) {
        this.socket = socket
    }

    fun reportInstallResult(appId: String, ok: Boolean, exitCode: Int, err: String?) {
        val trimmedId = appId.trim()
        if (trimmedId.isEmpty()) {
            return
        }
        val wire = mutableMapOf(
            "type" to "install_app_result",
            "app_id" to trimmedId,
            "ok" to ok,
            "exit_code" to exitCode,
        )
        if (!err.isNullOrBlank()) {
            wire["error"] = err
        }
        val json = gson.toJson(wire)
        val ws = socket
        if (ws == null) {
            Log.w(TAG, "Skipping install result uplink (socket offline): appId=$trimmedId")
            return
        }
        ws.send(json)
    }

    private const val TAG = "ArxC2Session"
}
