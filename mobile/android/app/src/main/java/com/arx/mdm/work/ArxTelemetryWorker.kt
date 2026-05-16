package com.arx.mdm.work

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.arx.mdm.network.ArxMtlsRetrofit
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.network.TelemetryService
import com.arx.mdm.PolicyManager
import com.arx.mdm.util.TelemetrySnapshot
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Periodic POST /v1/telemetry using the enrolled AndroidKeyStore client certificate (mTLS).
 */
class ArxTelemetryWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val ctx = applicationContext
        val state = ArxSecureState(ctx)
        if (!state.isMtlsEnrolled()) {
            return@withContext Result.success()
        }
        val server = state.getServerUrl()
        if (server.isNullOrBlank()) {
            Log.e(TAG, "missing server URL for telemetry")
            return@withContext Result.failure()
        }
        try {
            val retrofit = ArxMtlsRetrofit.androidKeyStoreMtls(server, state)
            val api = retrofit.create(TelemetryService::class.java)
            val body = TelemetrySnapshot.build(ctx)
            val response = api.postTelemetry(body)
            PolicyManager(ctx).applyServerPolicy(response.androidPolicy)
            state.setLastTelemetrySyncEpochMillis(System.currentTimeMillis())
            Result.success()
        } catch (e: Exception) {
            Log.e(TAG, "telemetry failed", e)
            Result.retry()
        }
    }

    companion object {
        const val UNIQUE_NAME: String = "arx_mdm_telemetry"
        private const val TAG = "ArxTelemetryWorker"
    }
}
