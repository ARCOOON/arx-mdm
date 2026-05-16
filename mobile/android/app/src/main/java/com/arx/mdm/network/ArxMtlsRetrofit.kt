package com.arx.mdm.network

import com.google.gson.GsonBuilder
import com.google.gson.JsonObject
import com.google.gson.annotations.SerializedName
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import java.security.KeyStore
import java.security.SecureRandom
import java.util.concurrent.TimeUnit
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

object ArxMtlsRetrofit {

    fun plainHttps(baseUrl: String): Retrofit {
        val client = OkHttpClient.Builder()
            .connectTimeout(30, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
            .callTimeout(120, TimeUnit.SECONDS)
            .build()
        return Retrofit.Builder()
            .baseUrl(normalizeBaseUrl(baseUrl))
            .client(client)
            .addConverterFactory(GsonConverterFactory.create(GsonBuilder().serializeNulls().create()))
            .build()
    }

    fun androidKeyStoreMtls(baseUrl: String, secure: ArxSecureState): Retrofit {
        val client = mtlsOkHttpClient(secure).newBuilder()
            .readTimeout(120, TimeUnit.SECONDS)
            .callTimeout(120, TimeUnit.SECONDS)
            .build()

        return Retrofit.Builder()
            .baseUrl(normalizeBaseUrl(baseUrl))
            .client(client)
            .addConverterFactory(GsonConverterFactory.create(GsonBuilder().serializeNulls().create()))
            .build()
    }

    /**
     * OkHttp instance that presents the enrollment client certificate chain (TLS mutual auth).
     * Read timeout is deliberately unbounded so long polling / websocket reads are not clipped.
     */
    fun mtlsOkHttpClient(secure: ArxSecureState): OkHttpClient {
        val tls = androidKeyStoreTls(secure)
        return OkHttpClient.Builder()
            .sslSocketFactory(tls.sslContext.socketFactory, tls.trustManager)
            .connectTimeout(30, TimeUnit.SECONDS)
            .readTimeout(0, TimeUnit.SECONDS)
            .writeTimeout(120, TimeUnit.SECONDS)
            .callTimeout(0, TimeUnit.SECONDS)
            .pingInterval(30, TimeUnit.SECONDS)
            .build()
    }

    private data class AndroidKeyStoreTls(
        val sslContext: SSLContext,
        val trustManager: X509TrustManager,
    )

    private fun androidKeyStoreTls(secure: ArxSecureState): AndroidKeyStoreTls {
        val rootPem = secure.getRootCaPem() ?: error("missing stored root CA PEM")
        val root = PemCertificates.parseChain(rootPem).first()
        val trustStore = KeyStore.getInstance(KeyStore.getDefaultType()).apply {
            load(null)
            setCertificateEntry("arx-mdm-root", root)
        }
        val tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        tmf.init(trustStore)
        val trustManager = tmf.trustManagers.first { it is X509TrustManager } as X509TrustManager

        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val kmf = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm())
        kmf.init(ks, null)

        val sslContext = SSLContext.getInstance("TLS").apply {
            init(kmf.keyManagers, tmf.trustManagers, SecureRandom())
        }
        return AndroidKeyStoreTls(sslContext, trustManager)
    }

    private fun normalizeBaseUrl(url: String): String {
        val trimmed = url.trim().trimEnd('/')
        require(
            trimmed.startsWith("https://", ignoreCase = true) ||
                trimmed.startsWith("http://", ignoreCase = true),
        ) {
            "MDM server URL must be an absolute http(s) URL"
        }
        return "$trimmed/"
    }
}

data class EnrollWireRequest(
    @SerializedName("csr") val csr: String,
    @SerializedName("token") val token: String,
)

data class EnrollWireResponse(
    @SerializedName("client_cert") val clientCert: String,
    @SerializedName("root_ca") val rootCa: String,
)

interface EnrollmentService {
    @POST("v1/enroll")
    suspend fun enroll(@Body body: EnrollWireRequest): EnrollWireResponse
}

data class TelemetryInstalledAppDto(
    @SerializedName("name") val name: String,
    @SerializedName("version") val version: String,
    @SerializedName("source") val source: String,
    @SerializedName("id") val id: String? = null,
)

data class TelemetryPayloadDto(
    @SerializedName("hostname") val hostname: String,
    @SerializedName("os_type") val osType: String = "android",
    @SerializedName("os_family") val osFamily: String = "android",
    @SerializedName("os_version") val osVersion: String,
    @SerializedName("total_ram_bytes") val totalRamBytes: Long,
    @SerializedName("cpu_model") val cpuModel: String,
    @SerializedName("cpu_logical_cores") val cpuLogicalCores: Int,
    @SerializedName("battery_percent") val batteryPercent: Double,
    @SerializedName("device_model") val deviceModel: String,
    @SerializedName("mac_address") val macAddress: String,
    @SerializedName("installed_software") val installedSoftware: List<TelemetryInstalledAppDto> = emptyList(),
    @SerializedName("mdm_policy_enforcement") val mdmPolicyEnforcement: MDMPolicyEnforcementDto? = null,
)

data class MDMPolicyEnforcementDto(
    @SerializedName("state") val state: String,
    @SerializedName("detail") val detail: String? = null,
)

data class EffectivePolicyWireDto(
    @SerializedName("effective_payload") val effectivePayload: JsonObject? = null,
)

interface EffectivePolicyService {
    @GET("v1/agent/effective-policy")
    suspend fun getEffectivePolicy(): EffectivePolicyWireDto
}

data class AndroidPolicyWireDto(
    @SerializedName("camera_disabled") val cameraDisabled: Boolean = false,
    @SerializedName("screen_lock_timeout_ms") val screenLockTimeoutMs: Long = 0L,
    @SerializedName("wipe_requested") val wipeRequested: Boolean = false,
)

data class TelemetryOkDto(
    @SerializedName("status") val status: String? = null,
    @SerializedName("asset_id") val assetId: String? = null,
    @SerializedName("human_id") val humanId: String? = null,
    @SerializedName("android_policy") val androidPolicy: AndroidPolicyWireDto? = null,
    @SerializedName("mdm_configuration_profiles") val mdmConfigurationProfiles: MutableList<JsonObject>? = null,
    @SerializedName("mdm_managed_app_configs") val mdmManagedAppConfigs: MutableList<JsonObject>? = null,
)

interface TelemetryService {
    @POST("v1/telemetry")
    suspend fun postTelemetry(@Body body: TelemetryPayloadDto): TelemetryOkDto
}
