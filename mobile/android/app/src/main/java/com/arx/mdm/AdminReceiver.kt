package com.arx.mdm

import android.app.admin.DeviceAdminReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.util.Log

/**
 * Handles device administration lifecycle and work-profile / device-owner provisioning completion.
 */
class AdminReceiver : DeviceAdminReceiver() {

    override fun onEnabled(context: Context, intent: Intent) {
        super.onEnabled(context, intent)
        Log.i(TAG, "Device admin enabled for ${componentName(context)}")
        AgentService.startOrRestart(context)
    }

    override fun onDisabled(context: Context, intent: Intent) {
        super.onDisabled(context, intent)
        Log.w(TAG, "Device admin disabled")
        AgentService.stopAgentGracefully(context)
    }

    override fun onDisableRequested(context: Context, intent: Intent): CharSequence? {
        Log.w(TAG, "Device admin disable requested")
        return super.onDisableRequested(context, intent)
    }

    override fun onProfileProvisioningComplete(context: Context, intent: Intent) {
        super.onProfileProvisioningComplete(context, intent)
        Log.i(TAG, "Profile provisioning complete; starting MDM agent service")
        AgentService.startOrRestart(context)
    }

    override fun onLockTaskModeEntering(context: Context, intent: Intent, pkg: String) {
        super.onLockTaskModeEntering(context, intent, pkg)
        Log.i(TAG, "Lock task entering: $pkg")
    }

    override fun onLockTaskModeExiting(context: Context, intent: Intent) {
        super.onLockTaskModeExiting(context, intent)
        Log.i(TAG, "Lock task exiting")
    }

    companion object {
        private const val TAG = "AdminReceiver"

        fun componentName(context: Context): ComponentName {
            return ComponentName(context, AdminReceiver::class.java)
        }
    }
}
