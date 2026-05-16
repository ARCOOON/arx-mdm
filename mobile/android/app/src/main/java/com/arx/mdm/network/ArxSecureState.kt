package com.arx.mdm.network

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * Encrypted preferences for provisioning secrets, enrollment CA trust material, and enrollment flags.
 * Client TLS private keys live only in [java.security.KeyStore] ("AndroidKeyStore").
 */
class ArxSecureState(context: Context) {

    private val appContext = context.applicationContext

    private val prefs: SharedPreferences by lazy {
        val masterKey = MasterKey.Builder(appContext)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            appContext,
            PREFS_FILE,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    fun setProvisioning(serverUrl: String, enrollmentToken: String) {
        prefs.edit()
            .putString(KEY_SERVER_URL, serverUrl)
            .putString(KEY_ENROLLMENT_TOKEN, enrollmentToken)
            .apply()
    }

    fun getServerUrl(): String? = prefs.getString(KEY_SERVER_URL, null)?.trim()?.takeIf { it.isNotEmpty() }

    fun getEnrollmentToken(): String? = prefs.getString(KEY_ENROLLMENT_TOKEN, null)?.trim()?.takeIf { it.isNotEmpty() }

    fun clearEnrollmentToken() {
        prefs.edit().remove(KEY_ENROLLMENT_TOKEN).apply()
    }

    fun setRootCaPem(pem: String) {
        prefs.edit().putString(KEY_ROOT_CA_PEM, pem).apply()
    }

    fun getRootCaPem(): String? = prefs.getString(KEY_ROOT_CA_PEM, null)?.trim()?.takeIf { it.isNotEmpty() }

    fun setMtlsEnrolled(enrolled: Boolean) {
        prefs.edit().putBoolean(KEY_MTLS_ENROLLED, enrolled).apply()
    }

    fun isMtlsEnrolled(): Boolean = prefs.getBoolean(KEY_MTLS_ENROLLED, false)

    fun setLastTelemetrySyncEpochMillis(millis: Long) {
        prefs.edit().putLong(KEY_LAST_TELEMETRY_SYNC_MS, millis).apply()
    }

    /** Wall-clock millis of last successful HTTP [TelemetryService.postTelemetry], or 0 if unknown. */
    fun getLastTelemetrySyncEpochMillis(): Long = prefs.getLong(KEY_LAST_TELEMETRY_SYNC_MS, 0L)

    companion object {
        private const val PREFS_FILE = "arx_mdm_secure_prefs"
        private const val KEY_SERVER_URL = "server_url"
        private const val KEY_ENROLLMENT_TOKEN = "enrollment_token"
        private const val KEY_ROOT_CA_PEM = "root_ca_pem"
        private const val KEY_MTLS_ENROLLED = "mtls_enrolled"
        private const val KEY_LAST_TELEMETRY_SYNC_MS = "last_telemetry_sync_epoch_ms"
    }
}
