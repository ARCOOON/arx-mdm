package com.arx.mdm.util

import java.net.NetworkInterface
import java.util.Collections

object MacAddressCollector {

    /**
     * Best-effort hardware MAC for a typical Wi-Fi interface. Many Android builds return
     * randomized or placeholder values for non-privileged callers.
     */
    fun primaryMacAddress(): String {
        return try {
            val ifs = Collections.list(NetworkInterface.getNetworkInterfaces())
            val preferred = ifs
                .filter { it.isUp && !it.isLoopback && !it.isVirtual }
                .sortedBy { nameRank(it.name) }
            for (nif in preferred) {
                val hw = nif.hardwareAddress ?: continue
                if (hw.isEmpty()) continue
                return hw.joinToString(":") { b -> "%02x".format(b) }
            }
            ""
        } catch (_: Exception) {
            ""
        }
    }

    private fun nameRank(name: String): Int {
        val n = name.lowercase()
        return when {
            n == "wlan0" -> 0
            n.startsWith("wlan") -> 1
            n.startsWith("eth") -> 2
            else -> 10
        }
    }
}
