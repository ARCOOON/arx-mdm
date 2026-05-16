package c2

var androidQuarantineHook func(enabled bool)

// SetAndroidQuarantineHook wires the Android device-owner implementation (gomobile shell registers this).
func SetAndroidQuarantineHook(fn func(enabled bool)) {
	androidQuarantineHook = fn
}

func invokeAndroidQuarantine(enabled bool) {
	if androidQuarantineHook != nil {
		androidQuarantineHook(enabled)
	}
}
