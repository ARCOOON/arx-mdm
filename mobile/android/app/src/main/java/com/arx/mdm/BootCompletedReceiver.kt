package com.arx.mdm

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.work.ArxTelemetryWorker
import java.util.concurrent.TimeUnit

class BootCompletedReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        if (intent?.action != Intent.ACTION_BOOT_COMPLETED) return
        val app = context.applicationContext
        if (!ArxSecureState(app).isMtlsEnrolled()) return
        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()
        val req = PeriodicWorkRequestBuilder<ArxTelemetryWorker>(15, TimeUnit.MINUTES)
            .setConstraints(constraints)
            .build()
        WorkManager.getInstance(app).enqueueUniquePeriodicWork(
            ArxTelemetryWorker.UNIQUE_NAME,
            ExistingPeriodicWorkPolicy.KEEP,
            req,
        )
    }
}
