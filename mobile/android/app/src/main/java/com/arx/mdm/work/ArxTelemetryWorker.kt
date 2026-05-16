package com.arx.mdm.work

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.arx.mdm.network.ArxMtlsRetrofit
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.network.EffectivePolicyService
import com.arx.mdm.network.TelemetryService
import com.arx.mdm.PolicyManager
import com.arx.mdm.policy.PolicyEnforcementReporter
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
            PolicyEnforcementReporter.resetOk()
            val retrofit = ArxMtlsRetrofit.androidKeyStoreMtls(server, state)
            val api = retrofit.create(TelemetryService::class.java)
            val policyManager = PolicyManager(ctx)

            try {
                val effectiveApi = retrofit.create(EffectivePolicyService::class.java)
                val effective = effectiveApi.getEffectivePolicy()
                val applied = policyManager.applyEffectiveDeclarativePayload(effective.effectivePayload)
                if (!applied) {
                    PolicyEnforcementReporter.reportError(
                        "effective declarative payload could not be applied on this device",
                    )
                }
            } catch (e: Exception) {
                PolicyEnforcementReporter.reportError(e.message ?: "effective policy fetch failed")
                Log.e(TAG, "effective policy sync failed", e)
            }

            val body = TelemetrySnapshot.build(ctx)
            val response = api.postTelemetry(body)
            policyManager.applyServerPolicy(response.androidPolicy)
            policyManager.applyDeclarativeProfilesFromTelemetry(null, response.mdmManagedAppConfigs)
            state.setLastTelemetrySyncEpochMillis(System.currentTimeMillis())
            Result.success()
        } catch (e: Exception) {
            PolicyEnforcementReporter.reportError(e.message ?: "telemetry sync failed")
            Log.e(TAG, "telemetry failed", e)
            Result.retry()
        }
    }

    companion object {
        const val UNIQUE_NAME: String = "arx_mdm_telemetry"
        private const val TAG = "ArxTelemetryWorker"
    }
}
