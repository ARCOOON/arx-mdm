package com.arx.mdm

import android.app.admin.DevicePolicyManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import com.arx.mdm.network.ArxSecureState
import com.arx.mdm.work.ArxEnrollmentWorker

/**
 * Receives [DevicePolicyManager.ACTION_PROVISIONING_COMPLETE], persists provisioning extras,
 * and kicks off mTLS enrollment against the MDM server.
 */
class ProvisioningCompleteReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent?) {
        if (intent == null) return
        if (DevicePolicyManager.ACTION_PROVISIONING_COMPLETE != intent.action) return

        val server = readExtra(intent, ArxProvisioningExtras.SERVER_URL)
        val token = readExtra(intent, ArxProvisioningExtras.ENROLLMENT_TOKEN)
        if (server.isNullOrBlank() || token.isNullOrBlank()) {
            Log.e(TAG, "Provisioning complete but missing server URL or enrollment token extras")
            return
        }

        ArxSecureState(context).setProvisioning(server.trim(), token.trim())
        Log.i(TAG, "Queued enrollment work for server ${server.trim()}")

        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()
        val req = OneTimeWorkRequestBuilder<ArxEnrollmentWorker>()
            .setConstraints(constraints)
            .build()
        WorkManager.getInstance(context.applicationContext).enqueueUniqueWork(
            ArxEnrollmentWorker.UNIQUE_NAME,
            ExistingWorkPolicy.REPLACE,
            req,
        )
    }

    private fun readExtra(intent: Intent, key: String): String? {
        val direct = intent.getStringExtra(key)
        if (!direct.isNullOrBlank()) return direct
        val bundleKey = "android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE"
        val bundle = intent.getBundleExtra(bundleKey) ?: return null
        return bundle.getString(key)
    }

    companion object {
        private const val TAG = "ArxProvisioning"
    }
}
