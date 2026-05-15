package com.arx.mdm

/**
 * Keys inside [android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE]; must match
 * `internal/api/enroll.go` (extraArxServerURL / extraArxEnrollmentToken).
 */
object ArxProvisioningExtras {
    const val SERVER_URL = "com.arx.mdm.EXTRA_SERVER_URL"
    const val ENROLLMENT_TOKEN = "com.arx.mdm.EXTRA_ENROLLMENT_TOKEN"
}
