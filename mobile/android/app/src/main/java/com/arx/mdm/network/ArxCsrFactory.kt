package com.arx.mdm.network

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import org.bouncycastle.asn1.x500.X500Name
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import org.bouncycastle.pkcs.jcajce.JcaPKCS10CertificationRequestBuilder
import org.bouncycastle.util.io.pem.PemObject
import org.bouncycastle.util.io.pem.PemWriter
import java.io.StringWriter
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.cert.X509Certificate
import javax.security.auth.x500.X500Principal

object ArxKeyAlias {
    const val MTLS: String = "arx_mdm_mtls"
}

object ArxCsrFactory {

    /**
     * Ensures a P-256 key in AndroidKeyStore under [ArxKeyAlias.MTLS] and returns a PEM CSR (SHA256withECDSA).
     */
    fun ensureKeyAndBuildCsrPem(): String {
        val pair = ensureEcKeyPair()
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val cert = ks.getCertificate(ArxKeyAlias.MTLS) as X509Certificate
        val subject = X500Name.getInstance(cert.subjectX500Principal.encoded)
        val priv = pair.private
        val signer = JcaContentSignerBuilder("SHA256withECDSA").build(priv)
        val csr = JcaPKCS10CertificationRequestBuilder(pair.public, subject).build(signer)
        val sw = StringWriter()
        PemWriter(sw).use { pw ->
            pw.writeObject(PemObject("CERTIFICATE REQUEST", csr.encoded))
        }
        return sw.toString()
    }

    private fun ensureEcKeyPair(): KeyPair {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val alias = ArxKeyAlias.MTLS
        if (ks.containsAlias(alias)) {
            val priv = ks.getKey(alias, null) as PrivateKey
            val pub = ks.getCertificate(alias).publicKey
            return KeyPair(pub, priv)
        }
        val safeModel = Build.MODEL.replace(',', '.').take(64)
        val subject = X500Principal("CN=ARX-MDM-$safeModel,O=ARX,C=US")
        val kpg = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(java.security.spec.ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setCertificateSubject(subject)
            .build()
        kpg.initialize(spec)
        return kpg.generateKeyPair()
    }
}
