package com.arx.mdm

import android.app.PendingIntent
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Build
import android.os.UserManager
import android.util.Log
import com.arx.mdm.network.AndroidPolicyWireDto
import com.arx.mdm.policy.ArxInstallResultReceiver
import com.google.gson.JsonObject
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
     * Suspends non-system packages except this MDM controller to approximate network isolation on Android.
     * Requires Android API 24 (N)+ device-owner privileges.
     */
    fun applyNetworkQuarantine(enabled: Boolean) {
        if (!isDeviceOwner()) {
            Log.w(TAG, "applyNetworkQuarantine ignored: not device owner")
            return
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) {
            Log.w(TAG, "applyNetworkQuarantine requires API 24+")
            return
        }
        val pm = context.packageManager
        val selfPkg = context.packageName
        val pkgs =
            pm.getInstalledApplications(PackageManager.GET_META_DATA).mapNotNull { app ->
                val pkg = app.packageName ?: return@mapNotNull null
                if (pkg == selfPkg) return@mapNotNull null
                val isSystem = (app.flags and ApplicationInfo.FLAG_SYSTEM) != 0
                if (isSystem) null else pkg
            }.distinct()
                .toTypedArray()
        try {
            dpm.setPackagesSuspended(admin, pkgs, enabled)
        } catch (e: SecurityException) {
            Log.e(TAG, "setPackagesSuspended denied", e)
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

    /**
     * Applies the merged effective declarative payload delivered by GET /v1/agent/effective-policy.
     */
    fun applyEffectiveDeclarativePayload(root: JsonObject?): Boolean {
        if (root == null || root.entrySet().isEmpty()) {
            return true
        }
        if (!isDeviceOwner()) {
            Log.w(TAG, "effective declarative skipped: not device owner")
            return false
        }
        applyDeclarativeProfilesFromTelemetry(listOf(root), null)
        return true
    }

    /**
     * Reconciles device-owner restrictions and Managed App Configuration bundles delivered as JSON.
     * Each heartbeat re-applies authoritative state so local tampering rolls back automatically.
     */
    fun applyDeclarativeProfilesFromTelemetry(
        profiles: List<JsonObject>?,
        managed: List<JsonObject>?,
    ) {
        if (profiles.isNullOrEmpty() && managed.isNullOrEmpty()) {
            return
        }
        if (!isDeviceOwner()) {
            Log.w(TAG, "Declarative payloads ignored without device-owner privilege")
            return
        }

        profiles?.let { reconcileUserRestrictionsFromTelemetry(it) }
        managed?.let { applyManagedAppConfigurations(it) }
    }

    private fun wiredRestrictionsCatalog(): Set<String> = setOf(
        UserManager.DISALLOW_INSTALL_UNKNOWN_SOURCES,
        UserManager.DISALLOW_INSTALL_APPS,
        UserManager.DISALLOW_UNINSTALL_APPS,
        UserManager.DISALLOW_USB_FILE_TRANSFER,
        UserManager.DISALLOW_DEBUGGING_FEATURES,
        UserManager.DISALLOW_CONFIG_WIFI,
        UserManager.DISALLOW_MOUNT_PHYSICAL_MEDIA,
    )

    private fun restrictionTokenToConstant(token: String): String? {
        val key = token.trim().replace('-', '_').lowercase(java.util.Locale.US)
        return when (key) {
            "install_unknown_sources", "no_install_unknown_sources" -> UserManager.DISALLOW_INSTALL_UNKNOWN_SOURCES
            "install_apps", "no_install_apps" -> UserManager.DISALLOW_INSTALL_APPS
            "uninstall_apps", "no_uninstall_apps" -> UserManager.DISALLOW_UNINSTALL_APPS
            "usb_file_transfer", "no_usb_file_transfer" -> UserManager.DISALLOW_USB_FILE_TRANSFER
            "debugging_features", "no_debugging_features" -> UserManager.DISALLOW_DEBUGGING_FEATURES
            "config_wifi", "no_config_wifi" -> UserManager.DISALLOW_CONFIG_WIFI
            "physical_media", "no_physical_media", "mount_physical_media" -> UserManager.DISALLOW_MOUNT_PHYSICAL_MEDIA
            else -> null
        }
    }

    private fun accumulateRestrictionDesired(profiles: List<JsonObject>): Set<String> {
        val union = mutableSetOf<String>()
        for (blob in profiles) {
            val payload = when {
                blob.has("payload") && blob["payload"].isJsonObject -> blob["payload"].asJsonObject
                else -> blob
            }
            val tokensFromRoot = payload.getAsJsonArray("user_restrictions")
            tokensFromRoot?.forEach rootLoop@{ el ->
                if (!el.isJsonPrimitive || !el.asJsonPrimitive.isString) return@rootLoop
                val mapped = restrictionTokenToConstant(el.asString) ?: return@rootLoop
                union.add(mapped)
            }

            payload.getAsJsonObject("android")?.getAsJsonArray("user_restrictions")?.forEach androidLoop@{ el ->
                if (!el.isJsonPrimitive || !el.asJsonPrimitive.isString) return@androidLoop
                val mapped = restrictionTokenToConstant(el.asString) ?: return@androidLoop
                union.add(mapped)
            }
        }
        return union.intersect(wiredRestrictionsCatalog())
    }

    private fun reconcileUserRestrictionsFromTelemetry(profiles: List<JsonObject>) {
        val desired = accumulateRestrictionDesired(profiles)
        for (restriction in wiredRestrictionsCatalog()) {
            try {
                if (desired.contains(restriction)) {
                    dpm.addUserRestriction(admin, restriction)
                } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    try {
                        dpm.clearUserRestriction(admin, restriction)
                    } catch (se: SecurityException) {
                        Log.w(TAG, "clearUserRestriction rejected for key=$restriction", se)
                    }
                }
            } catch (e: SecurityException) {
                Log.w(TAG, "addUserRestriction rejected for key=$restriction", e)
            }
        }
    }

    private fun applyManagedAppConfigurations(entries: List<JsonObject>) {
        entries.forEach { elem ->
            val pkg = elem.getAsJsonPrimitive("package_name")?.asString?.trim().orEmpty()
            if (pkg.isBlank()) return@forEach
            val kv = elem.getAsJsonObject("managed_config_kv") ?: JsonObject()
            val bundle = bundleFromFlatJson(kv)
            try {
                @Suppress("DEPRECATION")
                dpm.setApplicationRestrictions(admin, pkg, bundle)
            } catch (e: IllegalArgumentException) {
                Log.w(TAG, "Managed App Config rejected for pkg=$pkg", e)
            } catch (e: SecurityException) {
                Log.e(TAG, "Managed App Config blocked for pkg=$pkg", e)
            }
        }
    }

    private fun bundleFromFlatJson(obj: JsonObject): Bundle {
        val bundle = Bundle()
        obj.entrySet().forEach { entry ->
            val key = entry.key
            val valueElement = entry.value
            if (!valueElement.isJsonPrimitive) return@forEach
            val prim = valueElement.asJsonPrimitive
            when {
                prim.isBoolean -> bundle.putBoolean(key, prim.asBoolean)
                prim.isNumber -> {
                    val n = prim.asDouble
                    if (kotlin.math.abs(n - n.toLong()) < 1e-9) {
                        bundle.putLong(key, n.toLong())
                    } else {
                        bundle.putDouble(key, n)
                    }
                }
                prim.isString -> bundle.putString(key, prim.asString)
            }
        }
        return bundle
    }

    companion object {
        private const val TAG = "PolicyManager"
    }
}
