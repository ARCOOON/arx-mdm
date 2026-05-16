package com.arx.mdm.network

import java.security.KeyFactory
import java.security.KeyStore
import java.security.PrivateKey
import java.security.SecureRandom
import java.security.spec.PKCS8EncodedKeySpec
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

object ArxPemTls {

    private val privateKeyBlock = Regex(
        "-----BEGIN PRIVATE KEY-----\\s*(.+?)\\s*-----END PRIVATE KEY-----",
        setOf(RegexOption.DOT_MATCHES_ALL, RegexOption.MULTILINE),
    )

    /**
     * Pair of [SSLContext] and trust manager for use with OkHttp [okhttp3.OkHttpClient.Builder.sslSocketFactory].
     */
    fun sslContextAndTrustManagerFromPemFiles(
        clientKeyPem: String,
        clientCertPem: String,
        rootCaPem: String,
    ): Pair<SSLContext, X509TrustManager> {
        val privateKey = parsePkcs8EcPrivateKey(clientKeyPem)
        val chain = PemCertificates.parseChain(clientCertPem)
        val root = PemCertificates.parseChain(rootCaPem).first()

        val trustStore = KeyStore.getInstance(KeyStore.getDefaultType()).apply {
            load(null)
            setCertificateEntry("arx-mdm-root", root)
        }
        val tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        tmf.init(trustStore)
        val trustManager = tmf.trustManagers.first { it is X509TrustManager } as X509TrustManager

        val ks = KeyStore.getInstance(KeyStore.getDefaultType()).apply {
            load(null)
            setKeyEntry("client", privateKey, null, chain)
        }
        val kmf = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm())
        kmf.init(ks, null)

        val ctx = SSLContext.getInstance("TLS").apply {
            init(kmf.keyManagers, arrayOf(trustManager), SecureRandom())
        }
        return ctx to trustManager
    }

    fun sslContextFromPemFiles(
        clientKeyPem: String,
        clientCertPem: String,
        rootCaPem: String,
    ): SSLContext = sslContextAndTrustManagerFromPemFiles(clientKeyPem, clientCertPem, rootCaPem).first

    fun parsePkcs8EcPrivateKey(pem: String): PrivateKey {
        val m = privateKeyBlock.find(pem.trim())
            ?: throw IllegalArgumentException("PEM did not contain PKCS#8 PRIVATE KEY block")
        val b64 = m.groupValues[1].replace("\\s".toRegex(), "")
        val der = android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
        val spec = PKCS8EncodedKeySpec(der)
        val factory = KeyFactory.getInstance("EC")
        return factory.generatePrivate(spec)
    }

    private val publicKeyBlock = Regex(
        "-----BEGIN PUBLIC KEY-----\\s*(.+?)\\s*-----END PUBLIC KEY-----",
        setOf(RegexOption.DOT_MATCHES_ALL, RegexOption.MULTILINE),
    )

    fun parseEcPublicKeyPem(pem: String): java.security.PublicKey {
        val m = publicKeyBlock.find(pem.trim())
            ?: throw IllegalArgumentException("PEM did not contain PUBLIC KEY block")
        val b64 = m.groupValues[1].replace("\\s".toRegex(), "")
        val der = android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
        val spec = java.security.spec.X509EncodedKeySpec(der)
        val factory = KeyFactory.getInstance("EC")
        return factory.generatePublic(spec)
    }

    fun ecPublicKeyToPem(publicKey: java.security.PublicKey): String {
        val b64 = android.util.Base64.encodeToString(publicKey.encoded, android.util.Base64.NO_WRAP)
        val chunks = b64.chunked(64).joinToString("\n")
        return "-----BEGIN PUBLIC KEY-----\n$chunks\n-----END PUBLIC KEY-----\n"
    }
}
