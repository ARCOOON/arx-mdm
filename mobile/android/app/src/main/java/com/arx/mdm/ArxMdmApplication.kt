package com.arx.mdm

import android.app.Application
import android.util.Log
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.work.ArxTelemetryWorker
import org.bouncycastle.jce.provider.BouncyCastleProvider
import java.security.Security
import java.util.concurrent.TimeUnit

class ArxMdmApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        installBouncyCastle()
        scheduleTelemetryIfReady()
    }

    private fun installBouncyCastle() {
        if (Security.getProvider(BouncyCastleProvider.PROVIDER_NAME) == null) {
            Security.addProvider(BouncyCastleProvider())
            Log.i(TAG, "Registered BouncyCastle provider for CSR generation")
        }
    }

    private fun scheduleTelemetryIfReady() {
        val state = ArxSecureState(this)
        if (!state.isMtlsEnrolled()) return
        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()
        val req = PeriodicWorkRequestBuilder<ArxTelemetryWorker>(15, TimeUnit.MINUTES)
            .setConstraints(constraints)
            .build()
        WorkManager.getInstance(this).enqueueUniquePeriodicWork(
            ArxTelemetryWorker.UNIQUE_NAME,
            ExistingPeriodicWorkPolicy.KEEP,
            req,
        )
        AgentService.startOrRestart(this)
    }

    companion object {
        private const val TAG = "ArxMdmApplication"
    }
}
