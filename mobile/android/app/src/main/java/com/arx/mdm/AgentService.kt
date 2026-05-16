package com.arx.mdm

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Binder
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import com.arx.mdm.gomobile.GoAgentBridge
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.ui.C2ConnectionStatus
import com.arx.mdm.ui.DashboardUiState
import com.arx.mdm.ui.goC2StatusToConnectionStatus
import com.arx.mdm.work.ArxTelemetryWorker
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlin.math.max
import agentbind.Agentbind
import agentbind.AndroidSecurity

class AgentService : Service() {

    private val svcJob = SupervisorJob()
    private val scope = CoroutineScope(svcJob + Dispatchers.Default)
    private var refreshJob: Job? = null

    private val _dashboardState = MutableStateFlow(
        DashboardUiState.snapshot(this, C2ConnectionStatus.OFFLINE, null),
    )
    val dashboardState: StateFlow<DashboardUiState> = _dashboardState.asStateFlow()

    private val localBinder = LocalBinder()

    inner class LocalBinder : Binder() {
        fun getService(): AgentService = this@AgentService
    }

    override fun onBind(intent: Intent?): IBinder = localBinder

    override fun onCreate() {
        super.onCreate()
        ensureNotificationChannel()
        registerArxAndroidSecurityHooks()
        val fgType =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            } else {
                0
            }
        ServiceCompat.startForeground(
            this,
            NOTIFICATION_ID,
            buildForegroundNotification(),
            fgType,
        )
        startAgentIfEligible()
        refreshJob = scope.launch {
            while (isActive) {
                publishDashboardSnapshot()
                delay(1_000L)
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startAgentIfEligible()
        return START_STICKY
    }

    override fun onDestroy() {
        refreshJob?.cancel()
        scope.cancel()
        GoAgentBridge.stopAgent()
        super.onDestroy()
    }

    private fun registerArxAndroidSecurityHooks() {
        try {
            Agentbind.registerAndroidSecurity(object : AndroidSecurity {
                override fun lockDevice() {
                    PolicyManager(applicationContext).lockNow()
                }

                override fun wipeEnterprise() {
                    PolicyManager(applicationContext).wipeData()
                }
            })
        } catch (e: NoClassDefFoundError) {
            android.util.Log.w(
                TAG,
                "agentbind security hooks unavailable; rebuild agentbind.aar with gomobile",
                e,
            )
        } catch (e: Throwable) {
            android.util.Log.e(TAG, "registerAndroidSecurity failed", e)
        }
    }

    private fun publishDashboardSnapshot() {
        val secure = ArxSecureState(this)
        val goMs = GoAgentBridge.lastTelemetryUnixMilli()
        val httpMs = secure.getLastTelemetrySyncEpochMillis()
        val last = max(goMs, httpMs).takeIf { it > 0L }
        val status = goC2StatusToConnectionStatus(GoAgentBridge.c2Status())
        _dashboardState.value = DashboardUiState.snapshot(this, status, last)
    }

    private fun startAgentIfEligible() {
        val secure = ArxSecureState(this)
        val url = secure.getServerUrl()?.trim() ?: return
        if (!secure.isMtlsEnrolled()) return
        if (!ArxAgentCertPaths.hasCompleteMaterial(this)) return
        val dir = ArxAgentCertPaths.certDir(this).absolutePath
        GoAgentBridge.startAgent(url, dir)
    }

    fun forceReconnectWebSocket() {
        GoAgentBridge.forceReconnect()
    }

    fun requestImmediateTelemetryPush() {
        GoAgentBridge.syncTelemetryNow()
        val req = OneTimeWorkRequestBuilder<ArxTelemetryWorker>().build()
        WorkManager.getInstance(applicationContext).enqueue(req)
    }

    private fun ensureNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = getSystemService(NotificationManager::class.java)
        val ch = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.arx_c2_channel_name),
            NotificationManager.IMPORTANCE_LOW,
        )
        nm.createNotificationChannel(ch)
    }

    private fun buildForegroundNotification(): Notification {
        val pending = PendingIntent.getActivity(
            this,
            0,
            Intent(this, com.arx.mdm.ui.MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle(getString(R.string.arx_c2_notification_title))
            .setContentText(getString(R.string.arx_c2_notification_body))
            .setContentIntent(pending)
            .setOngoing(true)
            .build()
    }

    companion object {
        private const val CHANNEL_ID = "arx_mdm_c2"
        private const val NOTIFICATION_ID = 1001
        private const val TAG = "AgentService"

        fun startOrRestart(context: Context) {
            ContextCompat.startForegroundService(
                context,
                Intent(context, AgentService::class.java),
            )
        }

        fun stopAgentGracefully(context: Context) {
            context.stopService(Intent(context, AgentService::class.java))
        }
    }
}
