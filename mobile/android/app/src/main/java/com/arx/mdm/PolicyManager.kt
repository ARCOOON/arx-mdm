package com.arx.mdm

import android.app.PendingIntent
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build
import android.util.Log
import com.arx.mdm.network.AndroidPolicyWireDto
import com.arx.mdm.policy.ArxInstallResultReceiver
import java.io.File
import java.io.FileInputStream

/**
 * Device owner policy enforcement using [DevicePolicyManager] and the ARX admin [ComponentName].
 */
class PolicyManager(
    private val context: Context,
    private val dpm: DevicePolicyManager =
        context.getSystemService(Context.DEVICE_POLICY_SERVICE) as DevicePolicyManager,
    private val admin: ComponentName = AdminReceiver.componentName(context),
) {

    fun isDeviceOwner(): Boolean =
        dpm.isDeviceOwnerApp(context.packageName)

    /**
     * Sets minimum password quality (e.g. [DevicePolicyManager.PASSWORD_QUALITY_ALPHANUMERIC])
     * and optional minimum length when length is greater than zero.
     */
    fun setPasswordQualityAndLength(passwordQuality: Int, minimumLength: Int) {
        if (!isDeviceOwner()) {
            Log.w(TAG, "setPasswordQuality ignored: not device owner")
            return
        }
        try {
            dpm.setPasswordQuality(admin, passwordQuality)
            if (minimumLength > 0) {
                dpm.setMinimumPasswordLength(admin, minimumLength)
            }
        } catch (e: SecurityException) {
            Log.e(TAG, "setPasswordQuality denied", e)
        }
    }

    /**
     * Disables or enables all cameras on the device (device owner / API 30+).
     */
    fun setCameraDisabled(disabled: Boolean) {
        if (!isDeviceOwner()) {
            Log.w(TAG, "setCameraDisabled ignored: not device owner")
            return
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            Log.w(TAG, "setCameraDisabled requires API 30+")
            return
        }
        try {
            dpm.setCameraDisabled(admin, disabled)
        } catch (e: SecurityException) {
            Log.e(TAG, "setCameraDisabled denied", e)
        }
    }

    /**
     * Sets maximum time the device may be idle before locking (milliseconds). Skips if [timeoutMs] <= 0.
     */
    fun setMaximumTimeToLock(timeoutMs: Long) {
        if (!isDeviceOwner()) {
            Log.w(TAG, "setMaximumTimeToLock ignored: not device owner")
            return
        }
        if (timeoutMs <= 0L) {
            return
        }
        try {
            dpm.setMaximumTimeToLock(admin, timeoutMs)
        } catch (e: SecurityException) {
            Log.e(TAG, "setMaximumTimeToLock denied", e)
        }
    }

    /** Immediately locks the device (device owner). */
    fun lockNow() {
        if (!isDeviceOwner()) {
            Log.w(TAG, "lockNow ignored: not device owner")
            return
        }
        try {
            dpm.lockNow()
        } catch (e: SecurityException) {
            Log.e(TAG, "lockNow denied", e)
        }
    }

    /**
     * Factory reset / wipe. Requires device owner.
     */
    fun wipeData() {
        if (!isDeviceOwner()) {
            Log.w(TAG, "wipeData ignored: not device owner")
            return
        }
        try {
            dpm.wipeData(
                admin,
                0,
                "ARX MDM remote wipe",
            )
        } catch (e: SecurityException) {
            Log.e(TAG, "wipeData denied", e)
        }
    }

    /**
     * Silently installs an APK using [PackageInstaller] (device owner; no confirmation UI when supported).
     */
    fun installApkSilently(apkFile: File) {
        installApkSilently(apkFile, null)
    }

    /**
     * @param correlationAppId Optional catalog UUID used when reporting outcomes to the MDM uplink channel.
     */
    fun installApkSilently(apkFile: File, correlationAppId: String?) {
        if (!isDeviceOwner()) {
            Log.w(TAG, "installApkSilently ignored: not device owner")
            return
        }
        if (!apkFile.isFile) {
            Log.e(TAG, "installApkSilently: not a file: ${apkFile.path}")
            return
        }
        val pm = context.packageManager
        val installer = pm.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            params.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
        }
        var session: PackageInstaller.Session? = null
        try {
            val sessionId = installer.createSession(params)
            session = installer.openSession(sessionId)
            val sizeBytes = apkFile.length()
            FileInputStream(apkFile).use { input ->
                session.openWrite("package", 0, sizeBytes).use { out ->
                    input.copyTo(out)
                    session.fsync(out)
                }
            }
            val callbackIntent = Intent(ArxInstallResultReceiver.ACTION_INSTALL_COMPLETE).apply {
                setPackage(context.packageName)
                putExtra(PackageInstaller.EXTRA_SESSION_ID, sessionId)
                val cid = correlationAppId?.trim()
                if (!cid.isNullOrEmpty()) {
                    putExtra(ArxInstallResultReceiver.EXTRA_APP_CORRELATION_ID, cid)
                }
            }
            val piFlags = PendingIntent.FLAG_UPDATE_CURRENT or
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    PendingIntent.FLAG_MUTABLE
                } else {
                    0
                }
            val pendingIntent = PendingIntent.getBroadcast(
                context.applicationContext,
                sessionId,
                callbackIntent,
                piFlags,
            )
            session.commit(pendingIntent.intentSender)
        } catch (e: Exception) {
            Log.e(TAG, "installApkSilently failed", e)
            try {
                session?.abandon()
            } catch (_: Exception) {
            }
        }
    }

    /**
     * Applies server policy from telemetry downlink. Wipe is evaluated last.
     */
    fun applyServerPolicy(policy: AndroidPolicyWireDto?) {
        if (policy == null) {
            return
        }
        if (!isDeviceOwner()) {
            Log.w(TAG, "applyServerPolicy skipped: not device owner")
            return
        }
        setCameraDisabled(policy.cameraDisabled)
        setMaximumTimeToLock(policy.screenLockTimeoutMs)
        if (policy.wipeRequested) {
            wipeData()
        }
    }

    companion object {
        private const val TAG = "PolicyManager"
    }
}
