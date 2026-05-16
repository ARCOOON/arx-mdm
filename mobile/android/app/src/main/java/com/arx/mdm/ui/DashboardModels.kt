package com.arx.mdm.ui

import android.app.admin.DevicePolicyManager
import android.content.Context
import com.arx.mdm.ArxAgentCertPaths
import com.arx.mdm.gomobile.GoAgentBridge
import com.arx.mdm.network.ArxSecureState

enum class C2ConnectionStatus {
    OFFLINE,
    CONNECTING,
    ONLINE,
}

enum class MdmEnrollmentState {
    NOT_CONFIGURED,
    PENDING_ENROLLMENT,
    ENROLLED,
}

fun goC2StatusToConnectionStatus(raw: String): C2ConnectionStatus =
    when (raw.trim().lowercase()) {
        "online" -> C2ConnectionStatus.ONLINE
        "connecting" -> C2ConnectionStatus.CONNECTING
        else -> C2ConnectionStatus.OFFLINE
    }

data class DashboardUiState(
    val serverUrlDisplay: String,
    val connectionStatus: C2ConnectionStatus,
    val mdmEnrollmentState: MdmEnrollmentState,
    val deviceOwnerActive: Boolean,
    val mtlsClientCertPresent: Boolean,
    val rootCaStored: Boolean,
    val lastTelemetrySyncEpochMillis: Long?,
    val goBindVersionLine: String,
) {
    fun connectionLabel(): String =
        when (connectionStatus) {
            C2ConnectionStatus.ONLINE -> "Online"
            C2ConnectionStatus.CONNECTING -> "Connecting"
            C2ConnectionStatus.OFFLINE -> "Offline"
        }

    fun enrollmentLabel(): String =
        when (mdmEnrollmentState) {
            MdmEnrollmentState.ENROLLED -> "Enrolled (mTLS)"
            MdmEnrollmentState.PENDING_ENROLLMENT -> "Pending enrollment"
            MdmEnrollmentState.NOT_CONFIGURED -> "Not configured"
        }

    companion object {
        fun snapshot(context: Context, connectionStatus: C2ConnectionStatus, lastSync: Long? = null): DashboardUiState {
            val secure = ArxSecureState(context)
            val url = secure.getServerUrl()
            val pendingToken = secure.getEnrollmentToken()
            val enrolled = secure.isMtlsEnrolled()
            val enroll =
                when {
                    url.isNullOrBlank() -> MdmEnrollmentState.NOT_CONFIGURED
                    !enrolled && !pendingToken.isNullOrBlank() -> MdmEnrollmentState.PENDING_ENROLLMENT
                    enrolled -> MdmEnrollmentState.ENROLLED
                    else -> MdmEnrollmentState.PENDING_ENROLLMENT
                }
            val dpm = context.getSystemService(Context.DEVICE_POLICY_SERVICE) as DevicePolicyManager
            val owner = runCatching { dpm.isDeviceOwnerApp(context.packageName) }.getOrDefault(false)

            val pemReady = ArxAgentCertPaths.hasCompleteMaterial(context)
            val rootOk = !secure.getRootCaPem().isNullOrBlank()

            return DashboardUiState(
                serverUrlDisplay = url?.trim() ?: "—",
                connectionStatus = connectionStatus,
                mdmEnrollmentState = enroll,
                deviceOwnerActive = owner,
                mtlsClientCertPresent = pemReady,
                rootCaStored = rootOk,
                lastTelemetrySyncEpochMillis = lastSync,
                goBindVersionLine = "Go agent bind: ${GoAgentBridge.version()}",
            )
        }
    }
}

data class PingResult(
    val httpCode: Int,
    val latencyMs: Long,
    val errorMessage: String?,
) {
    fun summarize(): String {
        if (errorMessage != null) {
            return "Failed: $errorMessage (${latencyMs}ms)"
        }
        val note = if (httpCode == 405) " — GET not allowed (mTLS OK)" else ""
        return "HTTP $httpCode • ${latencyMs}ms$note"
    }
}
