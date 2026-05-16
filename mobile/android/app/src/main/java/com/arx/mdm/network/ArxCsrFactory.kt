package com.arx.mdm.network

import android.content.Context
import android.os.Build
import android.util.Log
import com.arx.mdm.ArxAgentCertPaths
import org.bouncycastle.asn1.x500.X500NameBuilder
import org.bouncycastle.asn1.x500.style.BCStyle
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import org.bouncycastle.pkcs.jcajce.JcaPKCS10CertificationRequestBuilder
import org.bouncycastle.util.io.pem.PemObject
import org.bouncycastle.util.io.pem.PemWriter
import java.io.File
import java.io.StringWriter
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.PrivateKey
import java.security.spec.ECGenParameterSpec

/**
 * Generates a secp256r1 key pair in app-private storage and builds an ECDSA CSR for enrollment.
 * Material is compatible with the Go agent ([internal/agent/mtls]) and [ArxPemTls].
 */
object ArxCsrFactory {

    private const val TAG = "ArxCsrFactory"

    fun ensureKeyAndBuildCsrPem(context: Context): String {
        val dir = ArxAgentCertPaths.certDir(context)
        val keyFile = File(dir, ArxAgentCertPaths.CLIENT_KEY)
        val pubFile = File(dir, ArxAgentCertPaths.CLIENT_PUB)
        val certHint = File(dir, ArxAgentCertPaths.CLIENT_CRT)

        val pair = when {
            keyFile.isFile && keyFile.length() > 0L && certHint.isFile && certHint.length() > 0L -> {
                loadPairFromKeyAndIssuedCert(keyFile.readText(), certHint.readText())
            }
            keyFile.isFile && keyFile.length() > 0L && pubFile.isFile && pubFile.length() > 0L -> {
                val priv = ArxPemTls.parsePkcs8EcPrivateKey(keyFile.readText())
                val pub = ArxPemTls.parseEcPublicKeyPem(pubFile.readText())
                KeyPair(pub, priv)
            }
            else -> {
                if (keyFile.exists()) keyFile.delete()
                if (pubFile.exists()) pubFile.delete()
                val kp = generateEcKeyPair()
                keyFile.writeText(pkcs8PrivatePem(kp.private))
                pubFile.writeText(ArxPemTls.ecPublicKeyToPem(kp.public))
                kp
            }
        }

        val subject = x500NameForDevice()
        val signer = JcaContentSignerBuilder("SHA256withECDSA").build(pair.private)
        val csr = JcaPKCS10CertificationRequestBuilder(subject, pair.public).build(signer)
        val sw = StringWriter()
        PemWriter(sw).use { pw ->
            pw.writeObject(PemObject("CERTIFICATE REQUEST", csr.encoded))
        }
        Log.i(TAG, "Built CSR for enrollment")
        return sw.toString()
    }

    private fun loadPairFromKeyAndIssuedCert(privateKeyPem: String, clientChainPem: String): KeyPair {
        val priv = ArxPemTls.parsePkcs8EcPrivateKey(privateKeyPem)
        val leaf = PemCertificates.parseChain(clientChainPem).first()
        val pub = leaf.publicKey
        return KeyPair(pub, priv)
    }

    private fun generateEcKeyPair(): KeyPair {
        val kpg = KeyPairGenerator.getInstance("EC", "BC")
        kpg.initialize(ECGenParameterSpec("secp256r1"))
        return kpg.generateKeyPair()
    }

    private fun pkcs8PrivatePem(privateKey: PrivateKey): String {
        val sw = StringWriter()
        PemWriter(sw).use { pw ->
            pw.writeObject(PemObject("PRIVATE KEY", privateKey.encoded))
        }
        return sw.toString()
    }

    private fun x500NameForDevice(): org.bouncycastle.asn1.x500.X500Name {
        val safeModel = Build.MODEL.replace(',', '.').take(64)
        return X500NameBuilder(BCStyle.INSTANCE)
            .addRDN(BCStyle.CN, "ARX-MDM-$safeModel")
            .addRDN(BCStyle.O, "ARX")
            .addRDN(BCStyle.C, "US")
            .build()
    }
}