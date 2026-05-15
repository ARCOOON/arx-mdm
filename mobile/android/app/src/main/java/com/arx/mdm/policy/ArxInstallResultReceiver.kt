package com.arx.mdm.policy

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.util.Log

/**
 * Receives [PackageInstaller] commit status for silent installs initiated by [PolicyManager].
 */
class ArxInstallResultReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_INSTALL_COMPLETE) {
            return
        }
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
        when (status) {
            PackageInstaller.STATUS_SUCCESS ->
                Log.i(TAG, "Package install session completed successfully")
            PackageInstaller.STATUS_PENDING_USER_ACTION ->
                Log.w(TAG, "Install requires user action (unexpected for silent path)")
            else ->
                Log.e(TAG, "Package install failed status=$status message=$message")
        }
    }

    companion object {
        const val ACTION_INSTALL_COMPLETE: String = "com.arx.mdm.PACKAGE_INSTALL_COMPLETE"
        private const val TAG = "ArxInstallResult"
    }
}
