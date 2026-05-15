package com.arx.mdm.util

import android.app.ActivityManager
import android.content.Context
import android.os.BatteryManager
import android.os.Build
import android.provider.Settings
import com.arx.mdm.network.TelemetryPayloadDto

object TelemetrySnapshot {

    fun build(context: Context): TelemetryPayloadDto {
        val androidId = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
            ?: "unknown"
        val hostname = "android-$androidId"

        val am = context.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
        val mem = ActivityManager.MemoryInfo()
        am.getMemoryInfo(mem)
        val totalRam = mem.totalMem.coerceAtLeast(0L)

        val batteryPct = readBatteryPercent(context)

        val model = "${Build.MANUFACTURER} ${Build.MODEL}".trim()

        return TelemetryPayloadDto(
            hostname = hostname,
            osType = "android",
            osFamily = "android",
            osVersion = Build.VERSION.RELEASE ?: "",
            totalRamBytes = totalRam,
            cpuModel = Build.HARDWARE ?: "",
            cpuLogicalCores = Runtime.getRuntime().availableProcessors().coerceAtLeast(1),
            batteryPercent = batteryPct,
            deviceModel = model,
            macAddress = MacAddressCollector.primaryMacAddress(),
            installedSoftware = emptyList(),
        )
    }

    private fun readBatteryPercent(context: Context): Double {
        val bm = context.getSystemService(Context.BATTERY_SERVICE) as BatteryManager
        val v = bm.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)
        return if (v in 0..100) v.toDouble() else -1.0
    }
}
