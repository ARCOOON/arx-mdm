package com.arx.mdm.policy

/**
 * Holds the latest declarative enforcement outcome for telemetry uplinks (thread-safe).
 */
object PolicyEnforcementReporter {
    private val lock = Any()
    private var state: String = "ok"
    private var detail: String? = null

    fun resetOk() {
        synchronized(lock) {
            state = "ok"
            detail = null
        }
    }

    fun reportError(message: String) {
        synchronized(lock) {
            state = "error"
            detail = message.trim().takeIf { it.isNotEmpty() }
        }
    }

    fun snapshot(): Pair<String, String?> {
        synchronized(lock) {
            return state to detail
        }
    }
}
