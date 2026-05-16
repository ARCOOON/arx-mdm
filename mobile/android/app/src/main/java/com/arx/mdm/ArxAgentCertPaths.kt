package com.arx.mdm

import android.content.Context
import java.io.File

/**
 * On-device paths for mutual-TLS material consumed by the Go agent ([internal/agent.ClientMaterialPaths])
 * and [com.arx.mdm.network.ArxMtlsRetrofit].
 */
object ArxAgentCertPaths {

    const val CLIENT_KEY: String = "client.key"
    /** Ephemeral EC public key (PEM) paired with [CLIENT_KEY] for CSR when [CLIENT_CRT] is not yet issued. */
    const val CLIENT_PUB: String = "client.pub.pem"
    const val CLIENT_CRT: String = "client.crt"
    const val ROOT_CA_PEM: String = "root_ca.pem"

    fun certDir(context: Context): File =
        File(context.filesDir, "certs").also { it.mkdirs() }

    fun clientKeyFile(context: Context): File = File(certDir(context), CLIENT_KEY)
    fun clientCertFile(context: Context): File = File(certDir(context), CLIENT_CRT)
    fun rootCaFile(context: Context): File = File(certDir(context), ROOT_CA_PEM)

    fun hasCompleteMaterial(context: Context): Boolean {
        val dir = certDir(context)
        return fileNonEmpty(File(dir, CLIENT_KEY)) &&
            fileNonEmpty(File(dir, CLIENT_CRT)) &&
            fileNonEmpty(File(dir, ROOT_CA_PEM))
    }

    private fun fileNonEmpty(f: File): Boolean = f.isFile && f.length() > 0L
}
