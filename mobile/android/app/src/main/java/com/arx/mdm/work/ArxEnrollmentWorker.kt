package com.arx.mdm.work

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkerParameters
import androidx.work.WorkManager
import com.arx.mdm.network.ArxCertificateInstaller
import com.arx.mdm.network.ArxCsrFactory
import com.arx.mdm.network.ArxMtlsRetrofit
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.network.EnrollWireRequest
import com.arx.mdm.network.EnrollmentService
import com.arx.mdm.AgentService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.util.concurrent.TimeUnit

/**
 * Performs one-shot CSR enrollment against POST /v1/enroll, installs the issued client certificate
 * into AndroidKeyStore, persists the MDM root CA for trust anchoring, then schedules telemetry.
 */
class ArxEnrollmentWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val ctx = applicationContext
        val state = ArxSecureState(ctx)
        val server = state.getServerUrl()
        val token = state.getEnrollmentToken()
        if (server.isNullOrBlank() || token.isNullOrBlank()) {
            Log.e(TAG, "missing server URL or enrollment token")
            return@withContext Result.failure()
        }
        if (state.isMtlsEnrolled()) {
            AgentService.startOrRestart(ctx)
            return@withContext Result.success()
        }
        try {
            val csr = ArxCsrFactory.ensureKeyAndBuildCsrPem()
            val retrofit = ArxMtlsRetrofit.plainHttps(server)
            val api = retrofit.create(EnrollmentService::class.java)
            val resp = api.enroll(EnrollWireRequest(csr = csr, token = token))
            ArxCertificateInstaller.installClientChain(resp.clientCert)
            state.setRootCaPem(resp.rootCa)
            state.setMtlsEnrolled(true)
            state.clearEnrollmentToken()

            val constraints = Constraints.Builder()
                .setRequiredNetworkType(NetworkType.CONNECTED)
                .build()
            val periodic = PeriodicWorkRequestBuilder<ArxTelemetryWorker>(15, TimeUnit.MINUTES)
                .setConstraints(constraints)
                .build()
            WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(
                ArxTelemetryWorker.UNIQUE_NAME,
                ExistingPeriodicWorkPolicy.KEEP,
                periodic,
            )
            AgentService.startOrRestart(ctx)
            Result.success()
        } catch (e: Exception) {
            Log.e(TAG, "enrollment failed", e)
            Result.retry()
        }
    }

    companion object {
        const val UNIQUE_NAME: String = "arx_mdm_enrollment"
        private const val TAG = "ArxEnrollmentWorker"
    }
}
