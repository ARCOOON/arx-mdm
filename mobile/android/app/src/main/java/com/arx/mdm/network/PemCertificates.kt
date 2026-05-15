package com.arx.mdm.network

import java.io.ByteArrayInputStream
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate

object PemCertificates {

    private val certBlock = Regex(
        "-----BEGIN CERTIFICATE-----\\s*(.+?)\\s*-----END CERTIFICATE-----",
        setOf(RegexOption.DOT_MATCHES_ALL, RegexOption.MULTILINE),
    )

    fun parseChain(pem: String): Array<X509Certificate> {
        val cf = CertificateFactory.getInstance("X509")
        val out = ArrayList<X509Certificate>()
        for (m in certBlock.findAll(pem)) {
            val b64 = m.groupValues[1].replace("\\s".toRegex(), "")
            val der = android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
            out.add(cf.generateCertificate(ByteArrayInputStream(der)) as X509Certificate)
        }
        require(out.isNotEmpty()) { "PEM contained no certificates" }
        return out.toTypedArray()
    }
}
