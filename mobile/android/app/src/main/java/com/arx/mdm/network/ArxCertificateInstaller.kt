package com.arx.mdm.network

import java.security.KeyStore
import java.security.PrivateKey

object ArxCertificateInstaller {

    /**
     * Associates the issued certificate chain from the enrollment response with the existing
     * AndroidKeyStore private key for [ArxKeyAlias.MTLS].
     */
    fun installClientChain(clientCertPem: String) {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val alias = ArxKeyAlias.MTLS
        require(ks.containsAlias(alias)) { "missing AndroidKeyStore key for alias $alias" }
        val privateKey = ks.getKey(alias, null) as PrivateKey
        val chain = PemCertificates.parseChain(clientCertPem)
        ks.setKeyEntry(alias, privateKey, null, chain)
    }
}
