package com.arx.mdm.network

import android.content.Context
import com.arx.mdm.ArxAgentCertPaths
import java.io.File

object ArxCertificateInstaller {

    /**
     * Persists the issued certificate chain and root CA as PEM files for the Go agent and OkHttp mTLS.
     * Filenames match [internal/agent.ClientMaterialPaths].
     */
    fun writeTlsMaterialFromEnrollment(context: Context, clientCertPem: String, rootCaPem: String) {
        val dir = ArxAgentCertPaths.certDir(context)
        File(dir, ArxAgentCertPaths.CLIENT_CRT).writeText(clientCertPem.trim())
        File(dir, ArxAgentCertPaths.ROOT_CA_PEM).writeText(rootCaPem.trim())
    }
}
