package com.arx.mdm.gomobile

import agentbind.Agentbind

/**
 * Thin Kotlin wrapper over the gomobile-derived [Agentbind] AAR (package [agentbind]).
 */
object GoAgentBridge {

    fun version(): String = runCatching { Agentbind.version() }.getOrElse { "unavailable" }

    fun c2Status(): String = runCatching { Agentbind.c2Status() }.getOrDefault("offline")

    fun lastTelemetryUnixMilli(): Long = runCatching { Agentbind.lastTelemetryUnixMilli() }.getOrDefault(0L)

    fun startAgent(serverUrl: String, certDirAbsolutePath: String) {
        runCatching { Agentbind.startAgent(serverUrl, certDirAbsolutePath) }
    }

    fun stopAgent() {
        runCatching { Agentbind.stopAgent() }
    }

    fun forceReconnect() {
        runCatching { Agentbind.forceReconnect() }
    }

    fun syncTelemetryNow() {
        runCatching { Agentbind.syncTelemetryNow() }
    }
}
